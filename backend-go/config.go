package main

import (
	"os"
	"time"
)

var (
	trinoURL                 = getenv("TRINO_URL", "http://trino1:8080")
	trinoUser                = getenv("TRINO_USER", "log_search")
	trinoPassword            = getenv("TRINO_PASSWORD", "")
	trinoCatalog             = getenv("TRINO_CATALOG", "iceberg")
	trinoSchema              = getenv("TRINO_SCHEMA", "logs")
	trinoSyslogTable         = getenv("TRINO_SYSLOG_TABLE", "syslog_events")
	trinoAuthlogTable        = getenv("TRINO_AUTHLOG_TABLE", "authlog_events")
	trinoTimestampColumn     = getenv("TRINO_TIMESTAMP_COLUMN", "ts")
	trinoTimestampExpression = getenv("TRINO_TIMESTAMP_EXPRESSION", "")
	jst                      = time.FixedZone("JST", 9*60*60)
	logTypes                 = []string{"syslog", "authlog"}
)

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
