package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
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
	defaultLimit             = getenvInt("TRINO_LIMIT", 50)
	jst                      = time.FixedZone("JST", 9*60*60)
	logTypes                 = []string{"syslog", "authlog"}
)

type App struct {
	client   TrinoExecutor
	template *template.Template
	sessions *SessionStore
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
	TimeFrom string `json:"time_from"`
	TimeTo   string `json:"time_to"`
	LogType  string `json:"log_type"`
	Host     string `json:"host"`
	Program  string `json:"program"`
	Message  string `json:"message"`
}

type LogRecord map[string]any

type PageData struct {
	Filters  Filters
	Logs     []LogRecord
	LogTypes []string
	Searched bool
}

type PendingSearch struct {
	Filters Filters
}

type SessionStore struct {
	mu    sync.Mutex
	items map[string]PendingSearch
}

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
	tpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		return nil, err
	}

	return &App{
		client:   client,
		template: tpl,
		sessions: &SessionStore{
			items: map[string]PendingSearch{},
		},
	}, nil
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.index)
	mux.HandleFunc("/clear", a.clearFilters)
	mux.HandleFunc("/health", a.health)
	mux.HandleFunc("/api/logs", a.apiSearchLogs)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
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
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		filters, err := filtersFromRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.sessions.Save(w, r, filters)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var filters Filters
	searched := false
	if r.URL.RawQuery != "" {
		filters = filtersFromValues(r.URL.Query())
		searched = true
	} else if pending, ok := a.sessions.Pop(w, r); ok {
		filters = pending.Filters
		searched = true
	} else {
		filters = normalizeFilters(Filters{})
	}

	var logs []LogRecord
	if searched {
		var err error
		logs, err = searchLogs(r.Context(), a.client, filters)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}

	data := PageData{
		Filters:  filters,
		Logs:     logs,
		LogTypes: logTypes,
		Searched: searched,
	}
	var rendered bytes.Buffer
	if err := a.template.Execute(&rendered, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(rendered.Bytes())
}

func (a *App) clearFilters(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.sessions.Clear(w, r)
	http.Redirect(w, r, "/", http.StatusFound)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	logs, err := searchLogs(r.Context(), a.client, filters)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, map[string]any{
		"filters": filters,
		"count":   len(logs),
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

func filtersFromValues(values url.Values) Filters {
	return normalizeFilters(Filters{
		TimeFrom: values.Get("time_from"),
		TimeTo:   values.Get("time_to"),
		LogType:  values.Get("log_type"),
		Host:     values.Get("host"),
		Program:  values.Get("program"),
		Message:  values.Get("message"),
	})
}

func normalizeFilters(filters Filters) Filters {
	return Filters{
		TimeFrom: strings.TrimSpace(filters.TimeFrom),
		TimeTo:   strings.TrimSpace(filters.TimeTo),
		LogType:  strings.TrimSpace(filters.LogType),
		Host:     strings.TrimSpace(filters.Host),
		Program:  strings.TrimSpace(filters.Program),
		Message:  strings.TrimSpace(filters.Message),
	}
}

func searchLogs(ctx context.Context, client TrinoExecutor, filters Filters) ([]LogRecord, error) {
	query := buildQuery(filters)
	rows, columns, err := client.Execute(ctx, query, 15*time.Second)
	if err != nil {
		return nil, err
	}

	logs := make([]LogRecord, 0, len(rows))
	for rowNumber, row := range rows {
		logRecord := LogRecord{
			"id":           rowNumber,
			"index":        trinoCatalog + "." + trinoSchema,
			"display_time": "",
		}
		for i, column := range columns {
			if i < len(row) {
				logRecord[column] = row[i]
			}
		}
		logRecord["display_time"] = formatTimestamp(logRecord["event_time"])
		logs = append(logs, logRecord)
	}
	return logs, nil
}

func buildQuery(filters Filters) string {
	selects := make([]string, 0, len(logTypes))
	for _, logType := range targetLogTypes(filters) {
		selects = append(selects, selectForLogType(logType, filters))
	}
	unionSQL := strings.Join(selects, "\nUNION ALL\n")
	return fmt.Sprintf("SELECT * FROM (\n%s\n) logs\nORDER BY event_time DESC\nLIMIT %d", unionSQL, defaultLimit)
}

func selectForLogType(logType string, filters Filters) string {
	timestampSQL := timestampExpressionSQL()
	conditions := []string{
		fmt.Sprintf("%s >= TIMESTAMP %s", timestampSQL, sqlString(timeBound(filters.TimeFrom, "from", time.Now().In(jst)))),
		fmt.Sprintf("%s <= TIMESTAMP %s", timestampSQL, sqlString(timeBound(filters.TimeTo, "to", time.Now().In(jst)))),
	}

	if filters.Host != "" {
		conditions = append(conditions, equalsCondition("host", filters.Host))
	}
	if filters.Program != "" {
		conditions = append(conditions, equalsCondition("program", filters.Program))
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

func (s *SessionStore) Save(w http.ResponseWriter, r *http.Request, filters Filters) {
	id := sessionID(r)
	if id == "" {
		id = randomID()
		http.SetCookie(w, &http.Cookie{
			Name:     "session_id",
			Value:    id,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = PendingSearch{Filters: filters}
}

func (s *SessionStore) Pop(w http.ResponseWriter, r *http.Request) (PendingSearch, bool) {
	id := sessionID(r)
	if id == "" {
		return PendingSearch{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.items[id]
	if ok {
		delete(s.items, id)
	}
	return pending, ok
}

func (s *SessionStore) Clear(w http.ResponseWriter, r *http.Request) {
	id := sessionID(r)
	if id != "" {
		s.mu.Lock()
		delete(s.items, id)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func sessionID(r *http.Request) string {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		return ""
	}
	return cookie.Value
}

func randomID() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
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

func getenvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
