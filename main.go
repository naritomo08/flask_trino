package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var logTypes = []string{"syslog", "authlog"}

type config struct {
	Port                string
	TrinoURL            string
	TrinoUser           string
	TrinoPassword       string
	TrinoCatalog        string
	TrinoSchema         string
	TrinoSyslogTable    string
	TrinoAuthlogTable   string
	TrinoTimestampCol   string
	TrinoTimestampExpr  string
	TrinoLimit          int
	TemplateDir         string
	StaticDir           string
	SessionCookieName   string
	SessionCookieSecure bool
}

type filters struct {
	TimeFrom string `json:"time_from"`
	TimeTo   string `json:"time_to"`
	LogType  string `json:"log_type"`
	Host     string `json:"host"`
	Program  string `json:"program"`
	Message  string `json:"message"`
}

type logRecord struct {
	ID          int    `json:"id"`
	Index       string `json:"index"`
	EventTime   any    `json:"event_time"`
	DisplayTime string `json:"display_time"`
	LogType     string `json:"log_type"`
	Host        string `json:"host"`
	Program     string `json:"program"`
	Msg         string `json:"msg"`
}

type templateData struct {
	Filters  filters
	Logs     []logRecord
	LogTypes []string
	Searched bool
	Error    string
}

type trinoClient struct {
	statementURL string
	httpClient   *http.Client
	cfg          config
}

type trinoResponse struct {
	ID      string          `json:"id"`
	InfoURI string          `json:"infoUri"`
	NextURI string          `json:"nextUri"`
	Columns []trinoColumn   `json:"columns"`
	Data    [][]any         `json:"data"`
	Error   *trinoError     `json:"error"`
	Stats   json.RawMessage `json:"stats"`
}

type trinoColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type trinoError struct {
	Message string `json:"message"`
}

type appServer struct {
	cfg      config
	client   queryClient
	template *template.Template
}

type queryClient interface {
	Ping() bool
	Execute(sql string) ([][]any, []string, error)
}

func main() {
	cfg := loadConfig()
	tmpl, err := parseTemplate(cfg.TemplateDir)
	if err != nil {
		log.Fatal(err)
	}

	server := &appServer{
		cfg:      cfg,
		client:   newTrinoClient(cfg),
		template: tmpl,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", server.handleIndex)
	mux.HandleFunc("/clear", server.handleClear)
	mux.HandleFunc("/health", server.handleHealth)
	mux.HandleFunc("/api/logs", server.handleAPILogs)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(cfg.StaticDir))))

	addr := ":" + cfg.Port
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func loadConfig() config {
	return config{
		Port:                getenv("PORT", "5000"),
		TrinoURL:            getenv("TRINO_URL", "http://trino1:8080"),
		TrinoUser:           getenv("TRINO_USER", "log_search"),
		TrinoPassword:       os.Getenv("TRINO_PASSWORD"),
		TrinoCatalog:        getenv("TRINO_CATALOG", "iceberg"),
		TrinoSchema:         getenv("TRINO_SCHEMA", "logs"),
		TrinoSyslogTable:    getenv("TRINO_SYSLOG_TABLE", "syslog_events"),
		TrinoAuthlogTable:   getenv("TRINO_AUTHLOG_TABLE", "authlog_events"),
		TrinoTimestampCol:   getenv("TRINO_TIMESTAMP_COLUMN", "ts"),
		TrinoTimestampExpr:  os.Getenv("TRINO_TIMESTAMP_EXPRESSION"),
		TrinoLimit:          getenvInt("TRINO_LIMIT", 50),
		TemplateDir:         getenv("TEMPLATE_DIR", "templates"),
		StaticDir:           getenv("STATIC_DIR", "static"),
		SessionCookieName:   "go_log_search_filters",
		SessionCookieSecure: os.Getenv("SESSION_COOKIE_SECURE") == "1",
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
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

func parseTemplate(templateDir string) (*template.Template, error) {
	return template.ParseFiles(templateDir + "/index.html")
}

func newTrinoClient(cfg config) *trinoClient {
	base := strings.TrimRight(cfg.TrinoURL, "/")
	return &trinoClient{
		statementURL: base + "/v1/statement",
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		cfg:          cfg,
	}
}

func (c *trinoClient) Ping() bool {
	_, _, err := c.Execute("SELECT 1")
	return err == nil
}

func (c *trinoClient) Execute(sql string) ([][]any, []string, error) {
	req, err := http.NewRequest(http.MethodPost, c.statementURL, bytes.NewBufferString(sql))
	if err != nil {
		return nil, nil, err
	}
	c.applyHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("trino statement failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var first trinoResponse
	if err := json.NewDecoder(resp.Body).Decode(&first); err != nil {
		return nil, nil, err
	}
	return c.collect(first)
}

func (c *trinoClient) collect(page trinoResponse) ([][]any, []string, error) {
	var rows [][]any
	var columns []string

	for {
		if page.Error != nil {
			return nil, nil, errors.New(page.Error.Message)
		}
		rows = append(rows, page.Data...)
		if len(columns) == 0 && len(page.Columns) > 0 {
			for _, column := range page.Columns {
				columns = append(columns, column.Name)
			}
		}
		if page.NextURI == "" {
			return rows, columns, nil
		}

		req, err := http.NewRequest(http.MethodGet, page.NextURI, nil)
		if err != nil {
			return nil, nil, err
		}
		c.applyHeaders(req)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, nil, fmt.Errorf("trino next page failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return nil, nil, err
		}
	}
}

func (c *trinoClient) applyHeaders(req *http.Request) {
	req.Header.Set("X-Trino-User", c.cfg.TrinoUser)
	req.Header.Set("X-Trino-Source", "go-trino-log-search")
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if c.cfg.TrinoCatalog != "" {
		req.Header.Set("X-Trino-Catalog", c.cfg.TrinoCatalog)
	}
	if c.cfg.TrinoSchema != "" {
		req.Header.Set("X-Trino-Schema", c.cfg.TrinoSchema)
	}
	if c.cfg.TrinoPassword != "" {
		req.SetBasicAuth(c.cfg.TrinoUser, c.cfg.TrinoPassword)
	}
}

func (s *appServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		f := normalizeFiltersFromValues(r.FormValue)
		s.setSearchCookie(w, f)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	f, searched := s.popSearchCookie(w, r)
	if len(r.URL.Query()) > 0 {
		f = normalizeFiltersFromValues(r.URL.Query().Get)
		searched = true
	}

	data := templateData{
		Filters:  f,
		LogTypes: logTypes,
		Searched: searched,
	}
	if searched {
		logs, err := searchLogs(s.client, s.cfg, f)
		if err != nil {
			data.Error = err.Error()
		} else {
			data.Logs = logs
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.template.Execute(w, data); err != nil {
		log.Printf("template render failed: %v", err)
	}
}

func (s *appServer) handleClear(w http.ResponseWriter, r *http.Request) {
	s.clearSearchCookie(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *appServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"ok":        s.client.Ping(),
		"trino_url": s.cfg.TrinoURL,
		"catalog":   s.cfg.TrinoCatalog,
		"schema":    s.cfg.TrinoSchema,
	})
}

func (s *appServer) handleAPILogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	f, err := filtersFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	logs, err := searchLogs(s.client, s.cfg, f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{
		"filters": f,
		"count":   len(logs),
		"logs":    logs,
	})
}

func filtersFromRequest(r *http.Request) (filters, error) {
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		defer r.Body.Close()
		var f filters
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil && !errors.Is(err, io.EOF) {
			return filters{}, err
		}
		return normalizeFilters(f), nil
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			return filters{}, err
		}
		return normalizeFiltersFromValues(r.PostForm.Get), nil
	}
	return normalizeFiltersFromValues(r.URL.Query().Get), nil
}

func normalizeFiltersFromValues(get func(string) string) filters {
	return normalizeFilters(filters{
		TimeFrom: get("time_from"),
		TimeTo:   get("time_to"),
		LogType:  get("log_type"),
		Host:     get("host"),
		Program:  get("program"),
		Message:  get("message"),
	})
}

func normalizeFilters(f filters) filters {
	return filters{
		TimeFrom: strings.TrimSpace(f.TimeFrom),
		TimeTo:   strings.TrimSpace(f.TimeTo),
		LogType:  strings.TrimSpace(f.LogType),
		Host:     strings.TrimSpace(f.Host),
		Program:  strings.TrimSpace(f.Program),
		Message:  strings.TrimSpace(f.Message),
	}
}

func (s *appServer) setSearchCookie(w http.ResponseWriter, f filters) {
	payload, _ := json.Marshal(f)
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.SessionCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(payload),
		Path:     "/",
		MaxAge:   60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.SessionCookieSecure,
	})
}

func (s *appServer) popSearchCookie(w http.ResponseWriter, r *http.Request) (filters, bool) {
	cookie, err := r.Cookie(s.cfg.SessionCookieName)
	if err != nil {
		return filters{}, false
	}
	s.clearSearchCookie(w)

	payload, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return filters{}, false
	}
	var f filters
	if err := json.Unmarshal(payload, &f); err != nil {
		return filters{}, false
	}
	return normalizeFilters(f), true
}

func (s *appServer) clearSearchCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.SessionCookieSecure,
	})
}

func searchLogs(client queryClient, cfg config, f filters) ([]logRecord, error) {
	sql := buildQuery(cfg, f, time.Now)
	rows, columns, err := client.Execute(sql)
	if err != nil {
		return nil, err
	}

	logs := make([]logRecord, 0, len(rows))
	for i, row := range rows {
		values := map[string]any{}
		for j, column := range columns {
			if j < len(row) {
				values[column] = row[j]
			}
		}

		logs = append(logs, logRecord{
			ID:          i,
			Index:       cfg.TrinoCatalog + "." + cfg.TrinoSchema,
			EventTime:   values["event_time"],
			DisplayTime: formatTimestamp(values["event_time"]),
			LogType:     stringValue(values["log_type"]),
			Host:        stringValue(values["host"]),
			Program:     stringValue(values["program"]),
			Msg:         stringValue(values["msg"]),
		})
	}
	return logs, nil
}

func buildQuery(cfg config, f filters, now func() time.Time) string {
	var selects []string
	for _, logType := range targetLogTypes(f) {
		selects = append(selects, selectForLogType(cfg, f, logType, now))
	}
	return fmt.Sprintf("SELECT * FROM (\n%s\n) logs\nORDER BY event_time DESC\nLIMIT %d", strings.Join(selects, "\nUNION ALL\n"), cfg.TrinoLimit)
}

func selectForLogType(cfg config, f filters, logType string, now func() time.Time) string {
	timestampSQL := timestampExpressionSQL(cfg)
	conditions := []string{
		fmt.Sprintf("%s >= TIMESTAMP %s", timestampSQL, sqlString(timeBound(f.TimeFrom, "from", now))),
		fmt.Sprintf("%s <= TIMESTAMP %s", timestampSQL, sqlString(timeBound(f.TimeTo, "to", now))),
	}
	if f.Host != "" {
		conditions = append(conditions, equalsCondition("host", f.Host))
	}
	if f.Program != "" {
		conditions = append(conditions, equalsCondition("program", f.Program))
	}
	if f.Message != "" {
		conditions = append(conditions, likeCondition("message", f.Message))
	}

	return fmt.Sprintf(`SELECT
  %[1]s AS event_time,
  CAST("host" AS varchar) AS host,
  CAST("program" AS varchar) AS program,
  CAST("message" AS varchar) AS msg,
  %[2]s AS log_type
FROM %[3]s
WHERE %[4]s`, timestampSQL, sqlString(logType), tableForLogType(cfg, logType), strings.Join(conditions, " AND "))
}

func targetLogTypes(f filters) []string {
	for _, logType := range logTypes {
		if f.LogType == logType {
			return []string{logType}
		}
	}
	return logTypes
}

func timeBound(value, direction string, now func() time.Time) string {
	jst := time.FixedZone("JST", 9*60*60)
	today := now().In(jst).Format("2006-01-02")
	value = strings.TrimSpace(value)
	if value == "" {
		if direction == "to" {
			return today + " 23:59:59"
		}
		return today + " 00:00:00"
	}

	if strings.Contains(value, "T") {
		if parsed, err := time.Parse("2006-01-02T15:04", value); err == nil {
			return parsed.Format("2006-01-02 15:04:05")
		}
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed.In(jst).Format("2006-01-02 15:04:05")
		}
	}

	layout := "15:04:05"
	if strings.Count(value, ":") == 1 {
		value += ":00"
	}
	if parsed, err := time.Parse(layout, value); err == nil {
		return today + " " + parsed.Format(layout)
	}

	if direction == "to" {
		return today + " 23:59:59"
	}
	return today + " 00:00:00"
}

func tableForLogType(cfg config, logType string) string {
	if logType == "syslog" {
		return tableExpr(cfg, cfg.TrinoSyslogTable)
	}
	return tableExpr(cfg, cfg.TrinoAuthlogTable)
}

func tableExpr(cfg config, name string) string {
	parts := splitNonEmpty(name, ".")
	if len(parts) == 1 {
		parts = []string{cfg.TrinoCatalog, cfg.TrinoSchema, name}
	}
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			quoted = append(quoted, quotedIdentifier(part))
		}
	}
	return strings.Join(quoted, ".")
}

func splitNonEmpty(value, separator string) []string {
	rawParts := strings.Split(value, separator)
	parts := make([]string, 0, len(rawParts))
	for _, part := range rawParts {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, strings.TrimSpace(part))
		}
	}
	return parts
}

func timestampExpressionSQL(cfg config) string {
	if cfg.TrinoTimestampExpr != "" {
		return cfg.TrinoTimestampExpr
	}
	return quotedIdentifier(cfg.TrinoTimestampCol)
}

func equalsCondition(field, value string) string {
	return fmt.Sprintf("lower(CAST(%s AS varchar)) = lower(%s)", quotedIdentifier(field), sqlString(value))
}

func likeCondition(field, value string) string {
	return fmt.Sprintf("lower(CAST(%s AS varchar)) LIKE lower(%s) ESCAPE '!'", quotedIdentifier(field), sqlString("%"+escapeLike(value)+"%"))
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

func formatTimestamp(value any) string {
	if value == nil {
		return ""
	}

	jst := time.FixedZone("JST", 9*60*60)
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return fmt.Sprint(typed)
		}
		return time.UnixMilli(int64(typed)).In(jst).Format("2006/01/02 15:04:05 JST")
	case int64:
		return time.UnixMilli(typed).In(jst).Format("2006/01/02 15:04:05 JST")
	case string:
		return formatTimestampString(typed)
	default:
		return fmt.Sprint(typed)
	}
}

func formatTimestampString(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	jst := time.FixedZone("JST", 9*60*60)
	replacements := []string{
		trimmed,
		strings.ReplaceAll(trimmed, " UTC", "Z"),
		strings.ReplaceAll(trimmed, " ", "T"),
		strings.ReplaceAll(strings.ReplaceAll(trimmed, " UTC", "Z"), " ", "T"),
	}
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}

	for _, candidate := range replacements {
		for _, layout := range layouts {
			if parsed, err := time.Parse(layout, candidate); err == nil {
				if strings.Contains(layout, "Z07") {
					return parsed.In(jst).Format("2006/01/02 15:04:05 JST")
				}
				return parsed.Format("2006/01/02 15:04:05 JST")
			}
		}
	}
	return value
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("json encode failed: %v", err)
	}
}
