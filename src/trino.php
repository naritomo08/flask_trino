<?php

declare(strict_types=1);

require_once __DIR__ . '/config.php';

function today_jst(): DateTimeImmutable
{
    return new DateTimeImmutable('today', new DateTimeZone(JST_TIMEZONE));
}

function add_seconds(string $value): string
{
    return substr_count($value, ':') === 1 ? "{$value}:00" : $value;
}

function time_bound(string $value, string $direction, ?DateTimeImmutable $targetDate = null): string
{
    $targetDate ??= today_jst();
    $boundary = $direction === 'from' ? '00:00:00' : '23:59:59';

    if ($value === '') {
        return $targetDate->format('Y-m-d') . " {$boundary}";
    }

    $normalized = trim($value);
    if (str_contains($normalized, 'T')) {
        try {
            $parsed = new DateTimeImmutable($normalized);
            return $parsed->setTimezone(new DateTimeZone(JST_TIMEZONE))->format('Y-m-d H:i:s');
        } catch (Exception) {
            // Fall through to time-only parsing.
        }
    }

    $time = DateTimeImmutable::createFromFormat('!H:i:s', add_seconds($normalized));
    if ($time instanceof DateTimeImmutable) {
        return $targetDate->format('Y-m-d') . ' ' . $time->format('H:i:s');
    }

    return $targetDate->format('Y-m-d') . " {$boundary}";
}

function format_timestamp(mixed $value): string
{
    if ($value === null || $value === '') {
        return '';
    }

    $jst = new DateTimeZone(JST_TIMEZONE);
    if (is_int($value) || is_float($value)) {
        $seconds = ((float) $value) / 1000;
        $date = DateTimeImmutable::createFromFormat('U.u', sprintf('%.6F', $seconds), new DateTimeZone('UTC'));
        return $date ? $date->setTimezone($jst)->format('Y/m/d H:i:s') . ' JST' : (string) $value;
    }

    if (is_string($value)) {
        $trimmed = trim($value);
        $normalized = str_replace([' UTC', ' '], ['Z', 'T'], $trimmed);
        $normalized = str_replace('Z', '+00:00', $normalized);
        try {
            $date = new DateTimeImmutable($normalized);
            if (!preg_match('/(?:Z|[+-]\d{2}:?\d{2})$/', $trimmed)) {
                return $date->format('Y/m/d H:i:s') . ' JST';
            }
            return $date->setTimezone($jst)->format('Y/m/d H:i:s') . ' JST';
        } catch (Exception) {
            return $trimmed;
        }
    }

    return (string) $value;
}

function quoted_identifier(string $value): string
{
    return '"' . str_replace('"', '""', $value) . '"';
}

function sql_string(string $value): string
{
    return "'" . str_replace("'", "''", $value) . "'";
}

function escape_like(string $value): string
{
    return str_replace(['!', '%', '_'], ['!!', '!%', '!_'], $value);
}

function timestamp_expression_sql(array $config): string
{
    return $config['trino_timestamp_expression'] !== ''
        ? $config['trino_timestamp_expression']
        : quoted_identifier($config['trino_timestamp_column']);
}

function table_expr(string $name, array $config): string
{
    $parts = array_values(array_filter(explode('.', $name), fn ($part) => $part !== ''));
    if (count($parts) === 1) {
        $parts = [$config['trino_catalog'], $config['trino_schema'], $name];
    }
    return implode('.', array_map('quoted_identifier', $parts));
}

function table_for_log_type(string $logType, array $config): string
{
    return table_expr($logType === 'syslog' ? $config['trino_syslog_table'] : $config['trino_authlog_table'], $config);
}

function equals_condition(string $field, string $value): string
{
    return 'lower(CAST(' . quoted_identifier($field) . ' AS varchar)) = lower(' . sql_string($value) . ')';
}

function like_condition(string $field, string $value): string
{
    return 'lower(CAST(' . quoted_identifier($field) . ' AS varchar)) LIKE lower(' . sql_string('%' . escape_like($value) . '%') . ") ESCAPE '!'";
}

function target_log_types(array $filters): array
{
    return in_array($filters['log_type'] ?? '', LOG_TYPES, true) ? [$filters['log_type']] : LOG_TYPES;
}

function select_for_log_type(string $logType, array $filters, array $config): string
{
    $timestampSql = timestamp_expression_sql($config);
    $conditions = [
        "{$timestampSql} >= TIMESTAMP " . sql_string(time_bound($filters['time_from'], 'from')),
        "{$timestampSql} <= TIMESTAMP " . sql_string(time_bound($filters['time_to'], 'to')),
    ];

    if ($filters['host'] !== '') {
        $conditions[] = equals_condition('host', $filters['host']);
    }
    if ($filters['program'] !== '') {
        $conditions[] = equals_condition('program', $filters['program']);
    }
    if ($filters['message'] !== '') {
        $conditions[] = like_condition('message', $filters['message']);
    }

    return "SELECT
  {$timestampSql} AS event_time,
  CAST(" . quoted_identifier('host') . " AS varchar) AS host,
  CAST(" . quoted_identifier('program') . " AS varchar) AS program,
  CAST(" . quoted_identifier('message') . " AS varchar) AS msg,
  " . sql_string($logType) . " AS log_type
FROM " . table_for_log_type($logType, $config) . '
WHERE ' . implode(' AND ', $conditions);
}

function build_query(array $filters, array $config): string
{
    $selects = array_map(fn ($logType) => select_for_log_type($logType, $filters, $config), target_log_types($filters));
    $unionSql = implode("\nUNION ALL\n", $selects);
    return "SELECT * FROM (\n{$unionSql}\n) logs\nORDER BY event_time DESC\nLIMIT {$config['default_limit']}";
}

function trino_headers(array $config): array
{
    $headers = [
        "X-Trino-User: {$config['trino_user']}",
        'X-Trino-Source: php-trino-log-search',
        'Content-Type: text/plain; charset=utf-8',
    ];
    if ($config['trino_catalog'] !== '') {
        $headers[] = "X-Trino-Catalog: {$config['trino_catalog']}";
    }
    if ($config['trino_schema'] !== '') {
        $headers[] = "X-Trino-Schema: {$config['trino_schema']}";
    }
    return $headers;
}

function trino_request(string $method, string $url, array $config, ?string $body = null, int $timeout = 15): array
{
    $curl = curl_init($url);
    $options = [
        CURLOPT_CUSTOMREQUEST => $method,
        CURLOPT_HTTPHEADER => trino_headers($config),
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_TIMEOUT => $timeout,
    ];
    if ($body !== null) {
        $options[CURLOPT_POSTFIELDS] = $body;
    }
    if ($config['trino_password'] !== '') {
        $options[CURLOPT_USERPWD] = "{$config['trino_user']}:{$config['trino_password']}";
    }

    curl_setopt_array($curl, $options);
    $response = curl_exec($curl);
    $status = curl_getinfo($curl, CURLINFO_RESPONSE_CODE);
    $error = curl_error($curl);
    curl_close($curl);

    if ($response === false || $status >= 400) {
        throw new RuntimeException($error !== '' ? $error : "Trino request failed with status {$status}");
    }

    $decoded = json_decode($response, true);
    if (!is_array($decoded)) {
        throw new RuntimeException('Trino returned invalid JSON');
    }
    return $decoded;
}

function collect_pages(array $body, array $config, int $timeout = 15): array
{
    $rows = [];
    $columns = [];

    while (true) {
        if (isset($body['error'])) {
            $message = is_array($body['error']) ? ($body['error']['message'] ?? json_encode($body['error'])) : (string) $body['error'];
            throw new RuntimeException("Trino query failed: {$message}");
        }

        if (isset($body['data']) && is_array($body['data'])) {
            $rows = array_merge($rows, $body['data']);
        }
        if (!$columns && isset($body['columns']) && is_array($body['columns'])) {
            $columns = array_map(fn ($column) => $column['name'], $body['columns']);
        }

        if (empty($body['nextUri'])) {
            return [$rows, $columns];
        }
        $body = trino_request('GET', $body['nextUri'], $config, null, $timeout);
    }
}

function trino_execute(string $sql, array $config, int $timeout = 15): array
{
    $statementUrl = rtrim($config['trino_url'], '/') . '/v1/statement';
    return collect_pages(trino_request('POST', $statementUrl, $config, $sql, $timeout), $config, $timeout);
}

function trino_ping(array $config): bool
{
    try {
        trino_execute('SELECT 1', $config, 5);
        return true;
    } catch (Throwable) {
        return false;
    }
}

function search_logs(array $filters, array $config): array
{
    [$rows, $columns] = trino_execute(build_query($filters, $config), $config);
    $logs = [];
    foreach ($rows as $index => $row) {
        $source = array_combine($columns, $row) ?: [];
        $source['id'] = $index;
        $source['index'] = "{$config['trino_catalog']}.{$config['trino_schema']}";
        $source['display_time'] = format_timestamp($source['event_time'] ?? null);
        $logs[] = $source;
    }
    return $logs;
}
