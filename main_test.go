package main

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakeTrino struct {
	queries []string
}

func (f *fakeTrino) Ping(ctx context.Context) bool {
	return true
}

func (f *fakeTrino) Execute(ctx context.Context, sql string, timeout time.Duration) ([][]any, []string, error) {
	f.queries = append(f.queries, sql)
	return [][]any{{
		"2026-06-02 20:11:55.000",
		"flink1",
		"systemd",
		"Reached target sshd-keygen.target.",
		"syslog",
	}}, []string{"event_time", "host", "program", "msg", "log_type"}, nil
}

func TestFormatTimestampConvertsEpochMillisToJST(t *testing.T) {
	got := formatTimestamp(int64(1780398715000))
	if got != "2026/06/02 20:11:55 JST" {
		t.Fatalf("formatTimestamp() = %q", got)
	}
}

func TestFormatTimestampKeepsNaiveTrinoTimestampAsJST(t *testing.T) {
	got := formatTimestamp("2026-06-02 20:11:55.000")
	if got != "2026/06/02 20:11:55 JST" {
		t.Fatalf("formatTimestamp() = %q", got)
	}
}

func TestTimeBoundUsesTodayJSTForTimeOnlyInput(t *testing.T) {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, jst)
	if got := timeBound("20:11", "from", now); got != "2026-06-02 20:11:00" {
		t.Fatalf("from bound = %q", got)
	}
	if got := timeBound("", "to", now); got != "2026-06-02 23:59:59" {
		t.Fatalf("to bound = %q", got)
	}
}

func TestEscapeHelpersQuoteSQLSafely(t *testing.T) {
	if got := sqlString("can't"); got != "'can''t'" {
		t.Fatalf("sqlString() = %q", got)
	}
	if got := quotedIdentifier(`bad"name`); got != `"bad""name"` {
		t.Fatalf("quotedIdentifier() = %q", got)
	}
	if got := escapeLike("100%!_"); got != "100!%!!!_" {
		t.Fatalf("escapeLike() = %q", got)
	}
}

func TestBuildQueryWithMessageProgramHostAndTimeRange(t *testing.T) {
	filters := Filters{
		TimeFrom: "20:00",
		TimeTo:   "21:00",
		LogType:  "syslog",
		Host:     "flink1",
		Program:  "systemd",
		Message:  "sshd",
	}

	query := buildQuery(filters)
	assertContains(t, query, `FROM "iceberg"."logs"."syslog_events"`)
	assertNotContains(t, query, `FROM "iceberg"."logs"."authlog_events"`)
	assertContains(t, query, `lower(CAST("host" AS varchar)) = lower('flink1')`)
	assertContains(t, query, `lower(CAST("program" AS varchar)) = lower('systemd')`)
	assertContains(t, query, `lower(CAST("message" AS varchar)) LIKE lower('%sshd%') ESCAPE '!'`)
	assertContains(t, query, "ORDER BY event_time DESC")
	assertContains(t, query, "LIMIT 50")
}

func TestBuildQuerySearchesBothLogTablesByDefault(t *testing.T) {
	query := buildQuery(normalizeFilters(Filters{Message: "authlog forward test from"}))
	assertContains(t, query, `FROM "iceberg"."logs"."syslog_events"`)
	assertContains(t, query, `FROM "iceberg"."logs"."authlog_events"`)
	assertContains(t, query, "UNION ALL")
	assertContains(t, query, "authlog forward test from")
}

func TestSearchLogsExecutesSQLAndFormatsResult(t *testing.T) {
	client := &fakeTrino{}
	logs, err := searchLogs(context.Background(), client, Filters{
		LogType: "syslog",
		Program: "systemd",
		Message: "sshd",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, client.queries[0], `FROM "iceberg"."logs"."syslog_events"`)
	assertNotContains(t, client.queries[0], `FROM "iceberg"."logs"."authlog_events"`)
	if logs[0]["display_time"] != "2026/06/02 20:11:55 JST" {
		t.Fatalf("display_time = %v", logs[0]["display_time"])
	}
	if logs[0]["log_type"] != "syslog" {
		t.Fatalf("log_type = %v", logs[0]["log_type"])
	}
}

func TestPostIndexSearchKeepsFiltersInBodyOnce(t *testing.T) {
	client := &fakeTrino{}
	app, err := NewApp(client)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.routes())
	defer server.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{Jar: jar}
	form := url.Values{"program": {"systemd"}, "message": {"sshd"}}
	resp, err := httpClient.PostForm(server.URL+"/", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body := responseBody(t, resp)
	assertContains(t, body, `method="post"`)
	assertContains(t, body, `id="search-form"`)
	assertContains(t, body, `id="results-summary"`)
	assertContains(t, body, `id="results-body"`)
	assertContains(t, body, `src="/static/search.js"`)
	assertContains(t, body, `type="time"`)
	assertContains(t, body, `value="systemd"`)
	assertContains(t, body, `value="sshd"`)
	assertContains(t, body, "2026/06/02 20:11:55 JST")
}

func TestPostAPILogsAcceptsJSON(t *testing.T) {
	client := &fakeTrino{}
	app, err := NewApp(client)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.routes())
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/logs", "application/json", strings.NewReader(`{"program":"systemd","message":"sshd"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body := responseBody(t, resp)
	assertContains(t, body, `"count":1`)
	assertContains(t, body, `"program":"systemd"`)
	assertContains(t, body, `"display_time":"2026/06/02 20:11:55 JST"`)
	assertContains(t, client.queries[0], `FROM "iceberg"."logs"."syslog_events"`)
	assertContains(t, client.queries[0], `FROM "iceberg"."logs"."authlog_events"`)
}

func assertContains(t *testing.T, value, needle string) {
	t.Helper()
	if !strings.Contains(value, needle) {
		t.Fatalf("expected %q to contain %q", value, needle)
	}
}

func assertNotContains(t *testing.T, value, needle string) {
	t.Helper()
	if strings.Contains(value, needle) {
		t.Fatalf("expected %q not to contain %q", value, needle)
	}
}

func responseBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	buf := new(strings.Builder)
	_, err := io.Copy(buf, resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
