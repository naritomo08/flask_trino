<?php

declare(strict_types=1);

const LOG_TYPES = ['syslog', 'authlog'];
const JST_TIMEZONE = 'Asia/Tokyo';

function app_config(): array
{
    return [
        'trino_url' => getenv('TRINO_URL') ?: 'http://trino1:8080',
        'trino_user' => getenv('TRINO_USER') ?: 'log_search',
        'trino_password' => getenv('TRINO_PASSWORD') ?: '',
        'trino_catalog' => getenv('TRINO_CATALOG') ?: 'iceberg',
        'trino_schema' => getenv('TRINO_SCHEMA') ?: 'logs',
        'trino_syslog_table' => getenv('TRINO_SYSLOG_TABLE') ?: 'syslog_events',
        'trino_authlog_table' => getenv('TRINO_AUTHLOG_TABLE') ?: 'authlog_events',
        'trino_timestamp_column' => getenv('TRINO_TIMESTAMP_COLUMN') ?: 'ts',
        'trino_timestamp_expression' => getenv('TRINO_TIMESTAMP_EXPRESSION') ?: '',
        'default_limit' => (int) (getenv('TRINO_LIMIT') ?: '50'),
    ];
}
