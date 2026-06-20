package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
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

type App struct {
	client TrinoExecutor
}

type TrinoExecutor interface {
	Ping(ctx context.Context) bool
	Execute(ctx context.Context, sql string, timeout time.Duration) ([][]any, []string, error)
}

type TrinoClient struct {
	statementURL string
	httpClient   *http.Client
}

type Filters struct {
	Date     string `json:"date"`
	TimeFrom string `json:"time_from"`
	TimeTo   string `json:"time_to"`
	LogType  string `json:"log_type"`
	Host     string `json:"host"`
	Program  string `json:"program"`
	Message  string `json:"message"`
	Page     int    `json:"page"`
	Size     int    `json:"size"`
}

type LogRecord map[string]any

type trinoColumn struct {
	Name string `json:"name"`
}

type trinoError struct {
	Message string `json:"message"`
}

type trinoResponse struct {
	Columns []trinoColumn `json:"columns"`
	Data    [][]any        `json:"data"`
	NextURI string         `json:"nextUri"`
	Error   *trinoError    `json:"error"`
}

func main() {
	app, err := NewApp(NewTrinoClient(trinoURL))
	if err != nil {
		log.Fatal(err)
	}

	server := &http.Server{
		Addr:              ":5000",
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func NewApp(client TrinoExecutor) (*App, error) {
	return &App{
		client: client,
	}, nil
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.index)
	mux.HandleFunc("/health", a.health)
	mux.HandleFunc("/api/options", a.apiOptions)
	mux.HandleFunc("/api/logs", a.apiSearchLogs)
	return mux
}

func NewTrinoClient(baseURL string) *TrinoClient {
	base := strings.TrimRight(baseURL, "/") + "/"
	statementURL, err := url.JoinPath(base, "v1", "statement")
	if err != nil {
		statementURL = base + "v1/statement"
	}
	return &TrinoClient{
		statementURL: statementURL,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

func (c *TrinoClient) Ping(ctx context.Context) bool {
	_, _, err := c.Execute(ctx, "SELECT 1", 5*time.Second)
	return err == nil
}

func (c *TrinoClient) Execute(ctx context.Context, sql string, timeout time.Duration) ([][]any, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.statementURL, strings.NewReader(sql))
	if err != nil {
		return nil, nil, err
	}
	setTrinoHeaders(req)
	setTrinoAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, nil, fmt.Errorf("trino query failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var body trinoResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, nil, err
	}
	return c.collectPages(ctx, body)
}

func (c *TrinoClient) collectPages(ctx context.Context, body trinoResponse) ([][]any, []string, error) {
	var rows [][]any
	var columns []string

	for {
		if body.Error != nil {
			message := body.Error.Message
			if message == "" {
				message = "unknown error"
			}
			return nil, nil, fmt.Errorf("trino query failed: %s", message)
		}

		rows = append(rows, body.Data...)
		if len(columns) == 0 && len(body.Columns) > 0 {
			for _, column := range body.Columns {
				columns = append(columns, column.Name)
			}
		}

		if body.NextURI == "" {
			return rows, columns, nil
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, body.NextURI, nil)
		if err != nil {
			return nil, nil, err
		}
		setTrinoHeaders(req)
		setTrinoAuth(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			resp.Body.Close()
			return nil, nil, fmt.Errorf("trino query failed: %s: %s", resp.Status, strings.TrimSpace(string(bodyBytes)))
		}

		var next trinoResponse
		err = json.NewDecoder(resp.Body).Decode(&next)
		resp.Body.Close()
		if err != nil {
			return nil, nil, err
		}
		body = next
	}
}

func setTrinoHeaders(req *http.Request) {
	req.Header.Set("X-Trino-User", trinoUser)
	req.Header.Set("X-Trino-Source", "go-trino-log-search")
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if trinoCatalog != "" {
		req.Header.Set("X-Trino-Catalog", trinoCatalog)
	}
	if trinoSchema != "" {
		req.Header.Set("X-Trino-Schema", trinoSchema)
	}
}

func setTrinoAuth(req *http.Request) {
	if trinoPassword != "" {
		req.SetBasicAuth(trinoUser, trinoPassword)
	}
}

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{
		"service":   "go-trino-backend",
		"endpoints": []string{"/health", "/api/options", "/api/logs"},
	})
}

func (a *App) apiOptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{
		"log_types": logTypes,
	})
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{
		"ok":        a.client.Ping(r.Context()),
		"trino_url": trinoURL,
		"catalog":   trinoCatalog,
		"schema":    trinoSchema,
	})
}

func (a *App) apiSearchLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filters, err := filtersFromRequest(r)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	logs, total, err := searchLogsPage(r.Context(), a.client, filters)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]any{
		"filters": filters,
		"count":   len(logs),
		"total":   total,
		"page":    filters.Page,
		"size":    filters.Size,
		"total_pages": int(math.Max(1, math.Ceil(float64(total)/float64(filters.Size)))),
		"logs":    logs,
	})
}

func filtersFromRequest(r *http.Request) (Filters, error) {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		var filters Filters
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&filters); err != nil && !errors.Is(err, io.EOF) {
				return Filters{}, err
			}
		}
		return normalizeFilters(filters), nil
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			return Filters{}, err
		}
		return filtersFromValues(r.PostForm), nil
	}

	return filtersFromValues(r.URL.Query()), nil
}

func filtersFromValues(values map[string][]string) Filters {
	get := func(key string) string {
		items := values[key]
		if len(items) == 0 {
			return ""
		}
		return items[0]
	}
	return normalizeFilters(Filters{
		Date:     get("date"),
		TimeFrom: get("time_from"),
		TimeTo:   get("time_to"),
		LogType:  get("log_type"),
		Host:     get("host"),
		Program:  get("program"),
		Message:  get("message"),
		Page:     parsePositiveInt(get("page"), 1),
		Size:     parsePositiveInt(get("size"), 25),
	})
}

func normalizeFilters(filters Filters) Filters {
	size := filters.Size
	if size <= 0 {
		size = 25
	}
	if size > 100 {
		size = 100
	}
	page := filters.Page
	if page <= 0 {
		page = 1
	}
	return Filters{
		Date:     strings.TrimSpace(filters.Date),
		TimeFrom: strings.TrimSpace(filters.TimeFrom),
		TimeTo:   strings.TrimSpace(filters.TimeTo),
		LogType:  strings.TrimSpace(filters.LogType),
		Host:     strings.TrimSpace(filters.Host),
		Program:  strings.TrimSpace(filters.Program),
		Message:  strings.TrimSpace(filters.Message),
		Page:     page,
		Size:     size,
	}
}

func parsePositiveInt(value string, fallback int) int {
	number, err := strconv.Atoi(value)
	if err != nil || number <= 0 {
		return fallback
	}
	return number
}

func searchLogs(ctx context.Context, client TrinoExecutor, filters Filters) ([]LogRecord, error) {
	query := buildQuery(filters)
	rows, columns, err := client.Execute(ctx, query, 60*time.Second)
	if err != nil {
		return nil, err
	}
	return rowsToLogs(rows, columns, filters), nil
}

func rowsToLogs(rows [][]any, columns []string, filters Filters) []LogRecord {
	logs := make([]LogRecord, 0, len(rows))
	for rowNumber, row := range rows {
		logRecord := LogRecord{
			"id":           (filters.Page-1)*filters.Size + rowNumber,
			"index":        trinoCatalog + "." + trinoSchema,
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
			total, _ = strconv.Atoi(value.String())
		default:
			total, _ = strconv.Atoi(fmt.Sprint(value))
		}
	}
	logs := rowsToLogs(rows, columns, filters)
	return logs, total, nil
}

func buildQuery(filters Filters) string {
	return fmt.Sprintf("SELECT logs.*, count(*) OVER() AS total_count FROM (\n%s\n) logs\nORDER BY event_time DESC\nOFFSET %d\nLIMIT %d",
		unionQuery(filters), (filters.Page-1)*filters.Size, filters.Size)
}

func buildCountQuery(filters Filters) string {
	return fmt.Sprintf("SELECT count(*) AS total FROM (\n%s\n) logs", unionQuery(filters))
}

func unionQuery(filters Filters) string {
	selects := make([]string, 0, len(logTypes))
	for _, logType := range targetLogTypes(filters) {
		selects = append(selects, selectForLogType(logType, filters))
	}
	unionSQL := strings.Join(selects, "\nUNION ALL\n")
	return unionSQL
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
		conditions = append(conditions, likeCondition("message", filters.Message))
	}

	return fmt.Sprintf(`SELECT
  %s AS event_time,
  CAST(%s AS varchar) AS host,
  CAST(%s AS varchar) AS program,
  CAST(%s AS varchar) AS msg,
  %s AS log_type
FROM %s
WHERE %s`, timestampSQL, quotedIdentifier("host"), quotedIdentifier("program"), quotedIdentifier("message"), sqlString(logType), tableForLogType(logType), strings.Join(conditions, " AND "))
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
	value = strings.ReplaceAll(value, "_", "!_")
	return value
}

func timeBound(value, direction string, now time.Time) string {
	targetDate := now.In(jst).Format("2006-01-02")
	if value == "" {
		if direction == "from" {
			return targetDate + " 00:00:00"
		}
		return targetDate + " 23:59:59"
	}

	normalized := strings.TrimSpace(value)
	if strings.Contains(normalized, "T") {
		if parsed, err := parseISOTime(normalized); err == nil {
			if parsed.Location() != time.Local {
				parsed = parsed.In(jst)
			}
			return parsed.Format("2006-01-02 15:04:05")
		}
	}

	if parsed, err := time.Parse("15:04:05", addSeconds(normalized)); err == nil {
		return targetDate + " " + parsed.Format("15:04:05")
	}

	if direction == "from" {
		return targetDate + " 00:00:00"
	}
	return targetDate + " 23:59:59"
}

func addSeconds(value string) string {
	if len(strings.Split(value, ":")) == 2 {
		return value + ":00"
	}
	return value
}

func parseISOTime(value string) (time.Time, error) {
	zonedLayouts := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
	}
	for _, layout := range zonedLayouts[:1] {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}

	for _, layout := range zonedLayouts[1:] {
		if parsed, err := time.ParseInLocation(layout, value, jst); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time: %s", value)
}

func formatTimestamp(value any) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case int64:
		return time.UnixMilli(v).UTC().In(jst).Format("2006/01/02 15:04:05 JST")
	case int:
		return time.UnixMilli(int64(v)).UTC().In(jst).Format("2006/01/02 15:04:05 JST")
	case float64:
		if math.Trunc(v) == v {
			return time.UnixMilli(int64(v)).UTC().In(jst).Format("2006/01/02 15:04:05 JST")
		}
	case json.Number:
		if millis, err := v.Int64(); err == nil {
			return time.UnixMilli(millis).UTC().In(jst).Format("2006/01/02 15:04:05 JST")
		}
	case time.Time:
		if v.Location() == time.Local {
			return v.Format("2006/01/02 15:04:05 JST")
		}
		return v.In(jst).Format("2006/01/02 15:04:05 JST")
	case string:
		if formatted, ok := formatTimestampString(v); ok {
			return formatted
		}
		return v
	}

	return fmt.Sprint(value)
}

func formatTimestampString(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	normalized := strings.ReplaceAll(trimmed, " UTC", "Z")
	normalized = strings.ReplaceAll(normalized, " ", "T")
	normalized = strings.ReplaceAll(normalized, "Z", "+00:00")

	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999-07:00",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, normalized); err == nil {
			if strings.Contains(layout, "-07:00") || strings.Contains(normalized, "+") {
				return parsed.In(jst).Format("2006/01/02 15:04:05 JST"), true
			}
			return parsed.Format("2006/01/02 15:04:05 JST"), true
		}
	}
	return "", false
}

func writeJSON(w http.ResponseWriter, value any) {
	writeJSONStatus(w, http.StatusOK, value)
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
