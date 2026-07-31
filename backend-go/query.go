package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func searchLogs(ctx context.Context, client TrinoExecutor, filters Filters) ([]LogRecord, error) {
	rows, columns, err := client.Execute(ctx, buildQuery(filters), 60*time.Second)
	if err != nil {
		return nil, err
	}
	return rowsToLogs(rows, columns, filters), nil
}

func rowsToLogs(rows [][]any, columns []string, filters Filters) []LogRecord {
	logs := make([]LogRecord, 0, len(rows))
	for rowNumber, row := range rows {
		logRecord := LogRecord{
			"id": (filters.Page-1)*filters.Size + rowNumber, "index": trinoCatalog + "." + trinoSchema,
			"display_time": "",
		}
		for i, column := range columns {
			if i < len(row) && column != "total_count" {
				logRecord[column] = row[i]
			}
		}
		logRecord["display_time"] = formatTimestamp(logRecord["event_time"])
		logs = append(logs, logRecord)
	}
	return logs
}

func searchLogsPage(ctx context.Context, client TrinoExecutor, filters Filters) ([]LogRecord, int, error) {
	rows, columns, err := client.Execute(ctx, buildQuery(filters), 60*time.Second)
	if err != nil {
		return nil, 0, err
	}
	total := 0
	totalIndex := -1
	for index, column := range columns {
		if column == "total_count" {
			totalIndex = index
			break
		}
	}
	if len(rows) > 0 && totalIndex >= 0 && totalIndex < len(rows[0]) {
		switch value := rows[0][totalIndex].(type) {
		case float64:
			total = int(value)
		case json.Number:
			total, _ = strconvAtoi(value.String())
		default:
			total, _ = strconvAtoi(fmt.Sprint(value))
		}
	}
	return rowsToLogs(rows, columns, filters), total, nil
}

func buildQuery(filters Filters) string {
	selectList := "logs.*, count(*) OVER() AS total_count"
	if filters.SkipTotal {
		selectList = "logs.*"
	}
	return fmt.Sprintf("SELECT %s FROM (\n%s\n) logs\nORDER BY event_time DESC\nOFFSET %d\nLIMIT %d", selectList,
		unionQuery(filters), (filters.Page-1)*filters.Size, filters.Size)
}

func buildCountQuery(filters Filters) string {
	return fmt.Sprintf("SELECT count(*) AS total FROM (\n%s\n) logs", unionQuery(filters))
}

func buildSummaryQuery(date string) string {
	target := time.Now().In(jst)
	if parsed, err := time.ParseInLocation("2006-01-02", date, jst); err == nil {
		target = parsed
	}
	day := sqlString(target.Format("2006-01-02"))
	return fmt.Sprintf("SELECT COALESCE(sum(total), 0) AS total FROM (\nSELECT COALESCE(sum(cnt), 0) AS total FROM %s WHERE dt = DATE %s\nUNION ALL\nSELECT COALESCE(sum(cnt), 0) AS total FROM %s WHERE dt = DATE %s\n) summaries",
		tableExpr(trinoSyslogHost1mTable), day, tableExpr(trinoAuthlogHost1mTable), day)
}

func getLogTotal(ctx context.Context, client TrinoExecutor, date string) (int, error) {
	rows, _, err := client.Execute(ctx, buildSummaryQuery(date), 60*time.Second)
	if err != nil || len(rows) == 0 || len(rows[0]) == 0 {
		return 0, err
	}
	switch value := rows[0][0].(type) {
	case float64:
		return int(value), nil
	case json.Number:
		total, _ := strconvAtoi(value.String())
		return total, nil
	default:
		total, _ := strconvAtoi(fmt.Sprint(value))
		return total, nil
	}
}

func unionQuery(filters Filters) string {
	selects := make([]string, 0, len(logTypes))
	for _, logType := range targetLogTypes(filters) {
		selects = append(selects, selectForLogType(logType, filters))
	}
	return strings.Join(selects, "\nUNION ALL\n")
}

func selectForLogType(logType string, filters Filters) string {
	timestampSQL := timestampExpressionSQL()
	targetDate := time.Now().In(jst)
	if parsed, err := time.ParseInLocation("2006-01-02", filters.Date, jst); err == nil {
		targetDate = parsed
	}
	conditions := []string{
		fmt.Sprintf("%s >= TIMESTAMP %s", timestampSQL, sqlString(timeBound(filters.TimeFrom, "from", targetDate))),
		fmt.Sprintf("%s <= TIMESTAMP %s", timestampSQL, sqlString(timeBound(filters.TimeTo, "to", targetDate))),
	}
	if filters.Host != "" {
		conditions = append(conditions, matchCondition("host", filters.Host))
	}
	if filters.Program != "" {
		conditions = append(conditions, matchCondition("program", filters.Program))
	}
	if filters.Message != "" {
		conditions = append(conditions, likeCondition("msg", filters.Message))
	}

	return fmt.Sprintf(`SELECT
  %s AS event_time,
  CAST(%s AS varchar) AS host,
  CAST(%s AS varchar) AS program,
  CAST(%s AS varchar) AS msg,
  %s AS log_type
FROM %s
WHERE %s`, timestampSQL, quotedIdentifier("host"), quotedIdentifier("program"), quotedIdentifier("msg"),
		sqlString(logType), tableForLogType(logType), strings.Join(conditions, " AND "))
}

func equalsCondition(field, value string) string {
	return fmt.Sprintf("lower(CAST(%s AS varchar)) = lower(%s)", quotedIdentifier(field), sqlString(value))
}

func matchCondition(field, value string) string {
	if len(value) >= 2 && strings.HasPrefix(value, "/") && strings.HasSuffix(value, "/") {
		return fmt.Sprintf("regexp_like(CAST(%s AS varchar), %s)", quotedIdentifier(field), sqlString("(?i)"+value[1:len(value)-1]))
	}
	return equalsCondition(field, value)
}

func likeCondition(field, value string) string {
	return fmt.Sprintf("lower(CAST(%s AS varchar)) LIKE lower(%s) ESCAPE '!'", quotedIdentifier(field), sqlString("%"+escapeLike(value)+"%"))
}

func targetLogTypes(filters Filters) []string {
	for _, logType := range logTypes {
		if filters.LogType == logType {
			return []string{logType}
		}
	}
	return logTypes
}

func tableForLogType(logType string) string {
	if logType == "syslog" {
		return tableExpr(trinoSyslogTable)
	}
	return tableExpr(trinoAuthlogTable)
}

func tableExpr(name string) string {
	parts := strings.Split(name, ".")
	filtered := make([]string, 0, len(parts)+2)
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	if len(filtered) == 1 {
		filtered = []string{trinoCatalog, trinoSchema, name}
	}
	quoted := make([]string, 0, len(filtered))
	for _, part := range filtered {
		if part != "" {
			quoted = append(quoted, quotedIdentifier(part))
		}
	}
	return strings.Join(quoted, ".")
}

func timestampExpressionSQL() string {
	if trinoTimestampExpression != "" {
		return trinoTimestampExpression
	}
	return quotedIdentifier(trinoTimestampColumn)
}

func quotedIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func sqlString(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, "!", "!!")
	value = strings.ReplaceAll(value, "%", "!%")
	return strings.ReplaceAll(value, "_", "!_")
}
