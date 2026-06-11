package main

import (
	"strings"
	"testing"
	"time"
)

type fakeClient struct {
	queries []string
}

func (f *fakeClient) Ping() bool {
	return true
}

func (f *fakeClient) Execute(sql string) ([][]any, []string, error) {
	f.queries = append(f.queries, sql)
	return [][]any{{
		"2026-06-02 20:11:55.000",
		"flink1",
		"systemd",
		"Reached target sshd-keygen.target.",
		"syslog",
	}}, []string{"event_time", "host", "program", "msg", "log_type"}, nil
}

func testConfig() config {
	return config{
		TrinoCatalog:      "iceberg",
		TrinoSchema:       "logs",
		TrinoSyslogTable:  "syslog_events",
		TrinoAuthlogTable: "authlog_events",
		TrinoTimestampCol: "ts",
		TrinoLimit:        50,
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 6, 2, 12, 0, 0, 0, time.FixedZone("JST", 9*60*60))
}

func TestFormatTimestampConvertsEpochMillisToJST(t *testing.T) {
	got := formatTimestamp(float64(1780398715000))
	want := "2026/06/02 20:11:55 JST"
	if got != want {
		t.Fatalf("formatTimestamp() = %q, want %q", got, want)
	}
}

func TestFormatTimestampKeepsNaiveTrinoTimestampAsJST(t *testing.T) {
	got := formatTimestamp("2026-06-02 20:11:55.000")
	want := "2026/06/02 20:11:55 JST"
	if got != want {
		t.Fatalf("formatTimestamp() = %q, want %q", got, want)
	}
}

func TestTimeBoundUsesTodayJSTForTimeOnlyInput(t *testing.T) {
	if got := timeBound("20:11", "from", fixedNow); got != "2026-06-02 20:11:00" {
		t.Fatalf("timeBound(from) = %q", got)
	}
	if got := timeBound("", "to", fixedNow); got != "2026-06-02 23:59:59" {
		t.Fatalf("timeBound(to) = %q", got)
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
	f := filters{
		TimeFrom: "20:00",
		TimeTo:   "21:00",
		LogType:  "syslog",
		Host:     "flink1",
		Program:  "systemd",
		Message:  "sshd",
	}

	query := buildQuery(testConfig(), f, fixedNow)

	assertContains(t, query, `FROM "iceberg"."logs"."syslog_events"`)
	assertNotContains(t, query, `FROM "iceberg"."logs"."authlog_events"`)
	assertContains(t, query, `"ts" >= TIMESTAMP '2026-06-02 20:00:00'`)
	assertContains(t, query, `"ts" <= TIMESTAMP '2026-06-02 21:00:00'`)
	assertContains(t, query, `lower(CAST("host" AS varchar)) = lower('flink1')`)
	assertContains(t, query, `lower(CAST("program" AS varchar)) = lower('systemd')`)
	assertContains(t, query, `lower(CAST("message" AS varchar)) LIKE lower('%sshd%') ESCAPE '!'`)
	assertContains(t, query, `ORDER BY event_time DESC`)
	assertContains(t, query, `LIMIT 50`)
}

func TestBuildQuerySearchesBothLogTablesByDefault(t *testing.T) {
	query := buildQuery(testConfig(), normalizeFilters(filters{Message: "authlog forward test from"}), fixedNow)

	assertContains(t, query, `FROM "iceberg"."logs"."syslog_events"`)
	assertContains(t, query, `FROM "iceberg"."logs"."authlog_events"`)
	assertContains(t, query, "UNION ALL")
	assertContains(t, query, "authlog forward test from")
}

func TestSearchLogsExecutesSQLAndFormatsResult(t *testing.T) {
	client := &fakeClient{}
	logs, err := searchLogs(client, testConfig(), filters{LogType: "syslog", Program: "systemd", Message: "sshd"})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(client.queries))
	}
	assertContains(t, client.queries[0], `FROM "iceberg"."logs"."syslog_events"`)
	assertNotContains(t, client.queries[0], `FROM "iceberg"."logs"."authlog_events"`)
	if logs[0].DisplayTime != "2026/06/02 20:11:55 JST" {
		t.Fatalf("DisplayTime = %q", logs[0].DisplayTime)
	}
	if logs[0].LogType != "syslog" || logs[0].Msg != "Reached target sshd-keygen.target." {
		t.Fatalf("unexpected log: %#v", logs[0])
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected %q to contain %q", haystack, needle)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("expected %q not to contain %q", haystack, needle)
	}
}
