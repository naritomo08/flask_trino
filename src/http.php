<?php

declare(strict_types=1);

use Psr\Http\Message\ResponseInterface as Response;
use Psr\Http\Message\ServerRequestInterface as Request;
use Slim\Factory\AppFactory;
use Slim\Psr7\Response as SlimResponse;

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

function html_response(Response $response, string $html, int $status = 200): Response
{
    $response->getBody()->write($html);
    return $response
        ->withHeader('Content-Type', 'text/html; charset=utf-8')
        ->withStatus($status);
}

function redirect_response(string $location, int $status = 302): Response
{
    return (new SlimResponse($status))->withHeader('Location', $location);
}

function render_view(string $template, array $data): string
{
    extract($data, EXTR_SKIP);
    ob_start();
    require __DIR__ . '/../views/' . $template . '.html';
    return (string) ob_get_clean();
}

function create_app(): \Slim\App
{
    session_start();

    $app = AppFactory::create();
    $app->addBodyParsingMiddleware();
    $app->addRoutingMiddleware();

    $config = app_config();

    $app->get('/', function (Request $request, Response $response) use ($config): Response {
        if ($request->getQueryParams()) {
            $filters = filters_from_request($request);
            $searched = true;
        } else {
            $searched = (bool) ($_SESSION['searched'] ?? false);
            $filters = $searched ? normalize_filters($_SESSION['filters'] ?? []) : normalize_filters([]);
            unset($_SESSION['filters'], $_SESSION['searched']);
        }

        $logs = $searched ? search_logs($filters, $config) : [];
        $html = render_view('index', [
            'filters' => $filters,
            'logs' => $logs,
            'options' => ['log_types' => LOG_TYPES],
            'searched' => $searched,
            'defaultLimit' => $config['default_limit'],
        ]);

        return html_response($response, $html);
    });

    $app->post('/', function (Request $request): Response {
        $_SESSION['filters'] = filters_from_request($request);
        $_SESSION['searched'] = true;
        return redirect_response('/');
    });

    $app->get('/clear', function (): Response {
        unset($_SESSION['filters'], $_SESSION['searched']);
        return redirect_response('/');
    });

    $app->get('/health', function (Request $request, Response $response) use ($config): Response {
        return json_response($response, [
            'ok' => trino_ping($config),
            'trino_url' => $config['trino_url'],
            'catalog' => $config['trino_catalog'],
            'schema' => $config['trino_schema'],
        ]);
    });

    $app->map(['GET', 'POST'], '/api/logs', function (Request $request, Response $response) use ($config): Response {
        $filters = filters_from_request($request);
        $logs = search_logs($filters, $config);
        return json_response($response, ['filters' => $filters, 'count' => count($logs), 'logs' => $logs]);
    });

    $app->addErrorMiddleware(true, true, true);

    return $app;
}
