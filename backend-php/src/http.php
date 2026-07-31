<?php

declare(strict_types=1);

use Psr\Http\Message\ResponseInterface as Response;
use Psr\Http\Message\ServerRequestInterface as Request;
use Slim\Factory\AppFactory;

const TRINO_UNAVAILABLE = [
    'error' => 'Trinoに接続できませんでした。稼働状況を確認して、もう一度お試しください。',
    'code' => 'trino_unavailable',
];

function normalize_filters(array $args): array
{
    return [
        'date' => trim((string) ($args['date'] ?? '')),
        'time_from' => trim((string) ($args['time_from'] ?? '')),
        'time_to' => trim((string) ($args['time_to'] ?? '')),
        'log_type' => trim((string) ($args['log_type'] ?? '')),
        'host' => trim((string) ($args['host'] ?? '')),
        'program' => trim((string) ($args['program'] ?? '')),
        'message' => trim((string) ($args['message'] ?? '')),
        'page' => positive_int($args['page'] ?? 1, 1),
        'size' => min(positive_int($args['size'] ?? 25, 25), 100),
        'skip_total' => in_array(strtolower((string) ($args['skip_total'] ?? '')), ['1', 'true'], true),
    ];
}

function positive_int(mixed $value, int $fallback): int
{
    $parsed = filter_var($value, FILTER_VALIDATE_INT);
    return is_int($parsed) && $parsed > 0 ? $parsed : $fallback;
}

function filters_from_request(Request $request): array
{
    $contentType = $request->getHeaderLine('Content-Type');
    if (str_contains($contentType, 'application/json')) {
        $payload = $request->getParsedBody();
        if ($payload === null) {
            $payload = json_decode((string) $request->getBody(), true);
        }
        return normalize_filters(is_array($payload) ? $payload : []);
    }

    if ($request->getMethod() === 'POST') {
        $payload = $request->getParsedBody();
        return normalize_filters(is_array($payload) ? $payload : []);
    }

    return normalize_filters($request->getQueryParams());
}

function json_response(Response $response, array $payload, int $status = 200): Response
{
    $response->getBody()->write(json_encode($payload, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES));
    return $response
        ->withHeader('Content-Type', 'application/json; charset=utf-8')
        ->withStatus($status);
}

function create_app(): \Slim\App
{
    $app = AppFactory::create();
    $app->addBodyParsingMiddleware();
    $app->addRoutingMiddleware();

    $config = app_config();

    $app->get('/', function (Request $request, Response $response) use ($config): Response {
        return json_response($response, [
            'service' => 'php-trino-backend',
            'endpoints' => ['/health', '/api/options', '/api/logs', '/api/summary'],
        ]);
    });

    $app->get('/health', function (Request $request, Response $response) use ($config): Response {
        return json_response($response, [
            'ok' => trino_ping($config),
            'trino_url' => $config['trino_url'],
            'catalog' => $config['trino_catalog'],
            'schema' => $config['trino_schema'],
        ]);
    });

    $app->get('/api/options', function (Request $request, Response $response): Response {
        return json_response($response, ['log_types' => LOG_TYPES]);
    });

    $app->map(['GET', 'POST'], '/api/logs', function (Request $request, Response $response) use ($config): Response {
        $filters = filters_from_request($request);
        try {
            $result = search_logs_page($filters, $config);
            return json_response($response, ['filters' => $filters, ...$result]);
        } catch (Throwable $error) {
            error_log('Trino log search failed: ' . $error);
            return json_response($response, TRINO_UNAVAILABLE, 502);
        }
    });

    $app->get('/api/summary', function (Request $request, Response $response) use ($config): Response {
        try {
            $date = trim((string) ($request->getQueryParams()['date'] ?? ''));
            return json_response($response, get_log_total($date, $config));
        } catch (Throwable $error) {
            error_log('Trino log summary failed: ' . $error);
            return json_response($response, TRINO_UNAVAILABLE, 502);
        }
    });

    $app->addErrorMiddleware(true, true, true);

    return $app;
}
