package com.example.flasktrino;

import org.junit.jupiter.api.Test;

import java.time.Clock;
import java.time.Instant;
import java.time.ZoneId;
import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

class AppTest {
    private static final Clock FIXED_CLOCK = Clock.fixed(Instant.parse("2026-06-02T03:00:00Z"), ZoneId.of("Asia/Tokyo"));

    @Test
    void formatTimestampConvertsEpochMillisToJst() {
        assertEquals("2026/06/02 20:11:55 JST", App.formatTimestamp(1780398715000L));
    }

    @Test
    void formatTimestampKeepsNaiveTrinoTimestampAsJst() {
        assertEquals("2026/06/02 20:11:55 JST", App.formatTimestamp("2026-06-02 20:11:55.000"));
    }

    @Test
    void timeBoundUsesTodayJstForTimeOnlyInput() {
        assertEquals("2026-06-02 20:11:00", App.timeBound("20:11", "from", FIXED_CLOCK));
        assertEquals("2026-06-02 23:59:59", App.timeBound("", "to", FIXED_CLOCK));
    }

    @Test
    void escapeHelpersQuoteSqlSafely() {
        assertEquals("'can''t'", App.sqlString("can't"));
        assertEquals("\"bad\"\"name\"", App.quotedIdentifier("bad\"name"));
        assertEquals("100!%!!!_", App.escapeLike("100%!_"));
    }

    @Test
    void buildQueryWithMessageProgramHostAndTimeRange() {
        App.Filters filters = new App.Filters("20:00", "21:00", "syslog", "flink1", "systemd", "sshd");

        String query = App.buildQuery(testConfig(), filters, FIXED_CLOCK);

        assertTrue(query.contains("FROM \"iceberg\".\"logs\".\"syslog_events\""));
        assertFalse(query.contains("FROM \"iceberg\".\"logs\".\"authlog_events\""));
        assertTrue(query.contains("\"ts\" >= TIMESTAMP '2026-06-02 20:00:00'"));
        assertTrue(query.contains("\"ts\" <= TIMESTAMP '2026-06-02 21:00:00'"));
        assertTrue(query.contains("lower(CAST(\"host\" AS varchar)) = lower('flink1')"));
        assertTrue(query.contains("lower(CAST(\"program\" AS varchar)) = lower('systemd')"));
        assertTrue(query.contains("lower(CAST(\"message\" AS varchar)) LIKE lower('%sshd%') ESCAPE '!'"));
        assertTrue(query.contains("ORDER BY event_time DESC"));
        assertTrue(query.contains("LIMIT 50"));
    }

    @Test
    void buildQuerySearchesBothLogTablesByDefault() {
        App.Filters filters = App.normalizeFilters(new App.Filters("", "", "", "", "", "authlog forward test from"));

        String query = App.buildQuery(testConfig(), filters, FIXED_CLOCK);

        assertTrue(query.contains("FROM \"iceberg\".\"logs\".\"syslog_events\""));
        assertTrue(query.contains("FROM \"iceberg\".\"logs\".\"authlog_events\""));
        assertTrue(query.contains("UNION ALL"));
        assertTrue(query.contains("authlog forward test from"));
    }

    @Test
    void searchLogsExecutesSqlAndFormatsResult() throws Exception {
        FakeClient client = new FakeClient();
        App.Filters filters = new App.Filters("", "", "syslog", "", "systemd", "sshd");

        List<App.LogRecord> logs = App.searchLogs(client, testConfig(), filters, FIXED_CLOCK);

        assertEquals(1, client.queries.size());
        assertTrue(client.queries.getFirst().contains("FROM \"iceberg\".\"logs\".\"syslog_events\""));
        assertFalse(client.queries.getFirst().contains("FROM \"iceberg\".\"logs\".\"authlog_events\""));
        assertEquals("2026/06/02 20:11:55 JST", logs.getFirst().displayTime());
        assertEquals("syslog", logs.getFirst().logType());
        assertEquals("Reached target sshd-keygen.target.", logs.getFirst().msg());
    }

    private static App.Config testConfig() {
        return new App.Config(
                "5000",
                "http://trino1:8080",
                "log_search",
                "",
                "iceberg",
                "logs",
                "syslog_events",
                "authlog_events",
                "ts",
                "",
                50,
                "static",
                "java_log_search_filters"
        );
    }

    static class FakeClient implements App.QueryClient {
        final List<String> queries = new java.util.ArrayList<>();

        @Override
        public boolean ping() {
            return true;
        }

        @Override
        public App.QueryResult execute(String sql) {
            queries.add(sql);
            return new App.QueryResult(
                    List.of(List.of(
                            "2026-06-02 20:11:55.000",
                            "flink1",
                            "systemd",
                            "Reached target sshd-keygen.target.",
                            "syslog"
                    )),
                    List.of("event_time", "host", "program", "msg", "log_type")
            );
        }
    }
}
