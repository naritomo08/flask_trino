package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
)

type App struct {
	client TrinoExecutor
}

var trinoUnavailable = map[string]any{
	"error": "Trinoに接続できませんでした。稼働状況を確認して、もう一度お試しください。",
	"code":  "trino_unavailable",
}

func NewApp(client TrinoExecutor) (*App, error) {
	return &App{client: client}, nil
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.index)
	mux.HandleFunc("/health", a.health)
	mux.HandleFunc("/api/options", a.apiOptions)
	mux.HandleFunc("/api/logs", a.apiSearchLogs)
	return mux
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
	writeJSON(w, map[string]any{"log_types": logTypes})
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
		log.Printf("Trino log search failed: %v", err)
		writeJSONStatus(w, http.StatusBadGateway, trinoUnavailable)
		return
	}

	writeJSON(w, map[string]any{
		"filters":     filters,
		"count":       len(logs),
		"total":       total,
		"page":        filters.Page,
		"size":        filters.Size,
		"total_pages": int(math.Max(1, math.Ceil(float64(total)/float64(filters.Size)))),
		"logs":        logs,
	})
}

func filtersFromRequest(r *http.Request) (Filters, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
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
		if len(values[key]) == 0 {
			return ""
		}
		return values[key][0]
	}
	return normalizeFilters(Filters{
		Date: get("date"), TimeFrom: get("time_from"), TimeTo: get("time_to"),
		LogType: get("log_type"), Host: get("host"), Program: get("program"), Message: get("message"),
		Page: parsePositiveInt(get("page"), 1), Size: parsePositiveInt(get("size"), 25),
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
		Date: strings.TrimSpace(filters.Date), TimeFrom: strings.TrimSpace(filters.TimeFrom),
		TimeTo: strings.TrimSpace(filters.TimeTo), LogType: strings.TrimSpace(filters.LogType),
		Host: strings.TrimSpace(filters.Host), Program: strings.TrimSpace(filters.Program),
		Message: strings.TrimSpace(filters.Message), Page: page, Size: size,
	}
}

func parsePositiveInt(value string, fallback int) int {
	number, err := strconv.Atoi(value)
	if err != nil || number <= 0 {
		return fallback
	}
	return number
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
