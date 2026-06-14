import Config

config :elixir_elastic,
  trino_url: System.get_env("TRINO_URL", "http://trino1:8080"),
  trino_user: System.get_env("TRINO_USER", "log_search"),
  trino_password: System.get_env("TRINO_PASSWORD", ""),
  trino_catalog: System.get_env("TRINO_CATALOG", "iceberg"),
  trino_schema: System.get_env("TRINO_SCHEMA", "logs"),
  syslog_table: System.get_env("TRINO_SYSLOG_TABLE", "syslog_events"),
  authlog_table: System.get_env("TRINO_AUTHLOG_TABLE", "authlog_events"),
  timestamp_column: System.get_env("TRINO_TIMESTAMP_COLUMN", "ts"),
  timestamp_expression: System.get_env("TRINO_TIMESTAMP_EXPRESSION", ""),
  session_secret: System.get_env("SESSION_SECRET", "dev-secret-key"),
  port: String.to_integer(System.get_env("PORT", "5000"))
