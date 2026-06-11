<?php

declare(strict_types=1);

require_once __DIR__ . '/trino.php';

function h(mixed $value): string
{
    return htmlspecialchars((string) $value, ENT_QUOTES | ENT_SUBSTITUTE, 'UTF-8');
}

function normalize_filters(array $args): array
{
    return [
        'time_from' => trim((string) ($args['time_from'] ?? '')),
        'time_to' => trim((string) ($args['time_to'] ?? '')),
        'log_type' => trim((string) ($args['log_type'] ?? '')),
        'host' => trim((string) ($args['host'] ?? '')),
        'program' => trim((string) ($args['program'] ?? '')),
        'message' => trim((string) ($args['message'] ?? '')),
    ];
}

function filters_from_request(): array
{
    $contentType = $_SERVER['CONTENT_TYPE'] ?? '';
    if (str_contains($contentType, 'application/json')) {
        $payload = json_decode(file_get_contents('php://input') ?: '{}', true);
        return normalize_filters(is_array($payload) ? $payload : []);
    }

    if ($_SERVER['REQUEST_METHOD'] === 'POST') {
        return normalize_filters($_POST);
    }

    return normalize_filters($_GET);
}

function json_response(array $payload, int $status = 200): never
{
    http_response_code($status);
    header('Content-Type: application/json; charset=utf-8');
    echo json_encode($payload, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
    exit;
}

function render_view(string $template, array $data): never
{
    extract($data, EXTR_SKIP);
    require __DIR__ . '/../views/' . $template . '.html';
    exit;
}

function handle_request(): never
{
    session_start();

    $config = app_config();
    $path = parse_url($_SERVER['REQUEST_URI'] ?? '/', PHP_URL_PATH) ?: '/';

    if ($path === '/clear') {
        unset($_SESSION['filters'], $_SESSION['searched']);
        header('Location: /', true, 302);
        exit;
    }

    if ($path === '/health') {
        json_response([
            'ok' => trino_ping($config),
            'trino_url' => $config['trino_url'],
            'catalog' => $config['trino_catalog'],
            'schema' => $config['trino_schema'],
        ]);
    }

    if ($path === '/api/logs') {
        $filters = filters_from_request();
        $logs = search_logs($filters, $config);
        json_response(['filters' => $filters, 'count' => count($logs), 'logs' => $logs]);
    }

    if ($path !== '/') {
        http_response_code(404);
        echo 'Not Found';
        exit;
    }

    if ($_SERVER['REQUEST_METHOD'] === 'POST') {
        $_SESSION['filters'] = filters_from_request();
        $_SESSION['searched'] = true;
        header('Location: /', true, 302);
        exit;
    }

    if ($_GET) {
        $filters = filters_from_request();
        $searched = true;
    } else {
        $searched = (bool) ($_SESSION['searched'] ?? false);
        $filters = $searched ? normalize_filters($_SESSION['filters'] ?? []) : normalize_filters([]);
        unset($_SESSION['filters'], $_SESSION['searched']);
    }

    $logs = $searched ? search_logs($filters, $config) : [];

    render_view('index', [
        'filters' => $filters,
        'logs' => $logs,
        'options' => ['log_types' => LOG_TYPES],
        'searched' => $searched,
        'defaultLimit' => $config['default_limit'],
    ]);
}
