package com.example.flasktrino;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.PropertyNamingStrategies;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.time.Clock;
import java.time.Instant;
import java.time.LocalDate;
import java.time.LocalDateTime;
import java.time.LocalTime;
import java.time.OffsetDateTime;
import java.time.ZoneId;
import java.time.format.DateTimeFormatter;
import java.time.format.DateTimeParseException;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.Executors;

public class App {
    static final List<String> LOG_TYPES = List.of("syslog", "authlog");
    static final ZoneId JST = ZoneId.of("Asia/Tokyo");
    static final DateTimeFormatter DISPLAY_TIME = DateTimeFormatter.ofPattern("yyyy/MM/dd HH:mm:ss 'JST'", Locale.ROOT);
    static final ObjectMapper JSON = new ObjectMapper().setPropertyNamingStrategy(PropertyNamingStrategies.SNAKE_CASE);

    private final Config config;
    private final QueryClient queryClient;
    private final Clock clock;

    public App(Config config, QueryClient queryClient, Clock clock) {
        this.config = config;
        this.queryClient = queryClient;
        this.clock = clock;
    }

    public static void main(String[] args) throws IOException {
        Config config = Config.fromEnv();
        App app = new App(config, new TrinoClient(config), Clock.systemUTC());
        app.start();
    }

    void start() throws IOException {
        HttpServer server = HttpServer.create(new InetSocketAddress(Integer.parseInt(config.port)), 0);
        server.createContext("/", this::handleIndex);
        server.createContext("/health", this::handleHealth);
        server.createContext("/api/options", this::handleApiOptions);
        server.createContext("/api/logs", this::handleApiLogs);
        server.setExecutor(Executors.newFixedThreadPool(16));
        server.start();
        System.out.printf("listening on :%s%n", config.port);
    }

    private void handleIndex(HttpExchange exchange) throws IOException {
        if (!"GET".equals(exchange.getRequestMethod())) {
            sendText(exchange, 405, "method not allowed", "text/plain; charset=utf-8");
            return;
        }
        Map<String, Object> payload = new LinkedHashMap<>();
        payload.put("service", "java-trino-backend");
        payload.put("endpoints", List.of("/health", "/api/options", "/api/logs"));
        sendJson(exchange, 200, payload);
    }

    private void handleHealth(HttpExchange exchange) throws IOException {
        Map<String, Object> payload = new LinkedHashMap<>();
        payload.put("ok", queryClient.ping());
        payload.put("trino_url", config.trinoUrl);
        payload.put("catalog", config.trinoCatalog);
        payload.put("schema", config.trinoSchema);
        sendJson(exchange, 200, payload);
    }

    private void handleApiOptions(HttpExchange exchange) throws IOException {
        if (!"GET".equals(exchange.getRequestMethod())) {
            sendText(exchange, 405, "method not allowed", "text/plain; charset=utf-8");
            return;
        }
        Map<String, Object> payload = new LinkedHashMap<>();
        payload.put("log_types", LOG_TYPES);
        sendJson(exchange, 200, payload);
    }

    private void handleApiLogs(HttpExchange exchange) throws IOException {
        String method = exchange.getRequestMethod();
        if (!"GET".equals(method) && !"POST".equals(method)) {
            sendText(exchange, 405, "method not allowed", "text/plain; charset=utf-8");
            return;
        }

        try {
            Filters filters = filtersFromRequest(exchange);
            SearchPage result = searchLogsPage(queryClient, config, filters, clock);
            Map<String, Object> payload = new LinkedHashMap<>();
            payload.put("filters", filters);
            payload.put("count", result.logs.size());
            payload.put("total", result.total);
            payload.put("page", filters.page);
            payload.put("size", filters.size);
            payload.put("total_pages", Math.max(1, (result.total + filters.size - 1) / filters.size));
            payload.put("logs", result.logs);
            sendJson(exchange, 200, payload);
        } catch (Exception ex) {
            sendJson(exchange, 502, Map.of("error", String.valueOf(ex.getMessage())));
        }
    }

    static List<LogRecord> searchLogs(QueryClient client, Config config, Filters filters, Clock clock) throws Exception {
        QueryResult result = client.execute(buildQuery(config, filters, clock));
        return rowsToLogs(result, config, filters);
    }

    static List<LogRecord> rowsToLogs(QueryResult result, Config config, Filters filters) {
        List<LogRecord> logs = new ArrayList<>();
        for (int i = 0; i < result.rows.size(); i++) {
            Map<String, Object> row = new LinkedHashMap<>();
            List<Object> values = result.rows.get(i);
            for (int j = 0; j < result.columns.size() && j < values.size(); j++) {
                row.put(result.columns.get(j), values.get(j));
            }
            logs.add(new LogRecord(
                    (filters.page - 1) * filters.size + i,
                    config.trinoCatalog + "." + config.trinoSchema,
                    row.get("event_time"),
                    formatTimestamp(row.get("event_time")),
                    stringValue(row.get("log_type")),
                    stringValue(row.get("host")),
                    stringValue(row.get("program")),
                    stringValue(row.get("msg"))
            ));
        }
        return logs;
    }

    static SearchPage searchLogsPage(QueryClient client, Config config, Filters filters, Clock clock) throws Exception {
        QueryResult result = client.execute(buildQuery(config, filters, clock));
        long total = 0;
        int totalIndex = result.columns.indexOf("total_count");
        if (!result.rows.isEmpty() && totalIndex >= 0 && totalIndex < result.rows.get(0).size()) {
            total = Long.parseLong(String.valueOf(result.rows.get(0).get(totalIndex)));
        }
        return new SearchPage(rowsToLogs(result, config, filters), total);
    }

    static String buildQuery(Config config, Filters filters, Clock clock) {
        return "SELECT logs.*, count(*) OVER() AS total_count FROM (\n"
                + unionQuery(config, filters, clock)
                + "\n) logs\nORDER BY event_time DESC\nOFFSET "
                + ((filters.page - 1) * filters.size)
                + "\nLIMIT "
                + filters.size;
    }

    static String buildCountQuery(Config config, Filters filters, Clock clock) {
        return "SELECT count(*) AS total FROM (\n" + unionQuery(config, filters, clock) + "\n) logs";
    }

    static String unionQuery(Config config, Filters filters, Clock clock) {
        List<String> selects = new ArrayList<>();
        for (String logType : targetLogTypes(filters)) {
            selects.add(selectForLogType(config, filters, logType, clock));
        }
        return String.join("\nUNION ALL\n", selects);
    }

    static String selectForLogType(Config config, Filters filters, String logType, Clock clock) {
        String timestampSql = timestampExpressionSql(config);
        List<String> conditions = new ArrayList<>();
        conditions.add(timestampSql + " >= TIMESTAMP " + sqlString(timeBound(filters.timeFrom, "from", clock, filters.date)));
        conditions.add(timestampSql + " <= TIMESTAMP " + sqlString(timeBound(filters.timeTo, "to", clock, filters.date)));
        if (!filters.host.isBlank()) {
            conditions.add(matchCondition("host", filters.host));
        }
        if (!filters.program.isBlank()) {
            conditions.add(matchCondition("program", filters.program));
        }
        if (!filters.message.isBlank()) {
            conditions.add(likeCondition("message", filters.message));
        }

        return """
                SELECT
                  %s AS event_time,
                  CAST("host" AS varchar) AS host,
                  CAST("program" AS varchar) AS program,
                  CAST("message" AS varchar) AS msg,
                  %s AS log_type
                FROM %s
                WHERE %s""".formatted(
                timestampSql,
                sqlString(logType),
                tableForLogType(config, logType),
                String.join(" AND ", conditions)
        );
    }

    static List<String> targetLogTypes(Filters filters) {
        if (LOG_TYPES.contains(filters.logType)) {
            return List.of(filters.logType);
        }
        return LOG_TYPES;
    }

    static String timeBound(String value, String direction, Clock clock) {
        return timeBound(value, direction, clock, "");
    }

    static String timeBound(String value, String direction, Clock clock, String date) {
        LocalDate today;
        try {
            today = LocalDate.parse(date);
        } catch (DateTimeParseException ex) {
            today = LocalDate.now(clock.withZone(JST));
        }
        String trimmed = value == null ? "" : value.trim();
        if (trimmed.isEmpty()) {
            return today + ("to".equals(direction) ? " 23:59:59" : " 00:00:00");
        }

        if (trimmed.contains("T")) {
            try {
                return LocalDateTime.parse(trimmed).format(DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss"));
            } catch (DateTimeParseException ignored) {
                try {
                    return OffsetDateTime.parse(trimmed).atZoneSameInstant(JST).format(DateTimeFormatter.ofPattern("yyyy-MM-dd HH:mm:ss"));
                } catch (DateTimeParseException ignoredAgain) {
                    // Fall through to time-only parsing.
                }
            }
        }

        String withSeconds = trimmed.chars().filter(ch -> ch == ':').count() == 1 ? trimmed + ":00" : trimmed;
        try {
            LocalTime parsed = LocalTime.parse(withSeconds);
            return today + " " + parsed.format(DateTimeFormatter.ofPattern("HH:mm:ss"));
        } catch (DateTimeParseException ignored) {
            return today + ("to".equals(direction) ? " 23:59:59" : " 00:00:00");
        }
    }

    static String tableForLogType(Config config, String logType) {
        return tableExpr(config, "syslog".equals(logType) ? config.trinoSyslogTable : config.trinoAuthlogTable);
    }

    static String tableExpr(Config config, String name) {
        List<String> parts = splitNonEmpty(name, "\\.");
        if (parts.size() == 1) {
            parts = List.of(config.trinoCatalog, config.trinoSchema, name);
        }
        return parts.stream().filter(part -> !part.isBlank()).map(App::quotedIdentifier).reduce((a, b) -> a + "." + b).orElse("");
    }

    static List<String> splitNonEmpty(String value, String regex) {
        List<String> parts = new ArrayList<>();
        for (String part : value.split(regex)) {
            String trimmed = part.trim();
            if (!trimmed.isEmpty()) {
                parts.add(trimmed);
            }
        }
        return parts;
    }

    static String timestampExpressionSql(Config config) {
        return config.trinoTimestampExpression.isBlank() ? quotedIdentifier(config.trinoTimestampColumn) : config.trinoTimestampExpression;
    }

    static String equalsCondition(String field, String value) {
        return "lower(CAST(%s AS varchar)) = lower(%s)".formatted(quotedIdentifier(field), sqlString(value));
    }

    static String matchCondition(String field, String value) {
        if (value.length() >= 2 && value.startsWith("/") && value.endsWith("/")) {
            return "regexp_like(CAST(%s AS varchar), %s)".formatted(
                    quotedIdentifier(field),
                    sqlString("(?i)" + value.substring(1, value.length() - 1))
            );
        }
        return equalsCondition(field, value);
    }

    static String likeCondition(String field, String value) {
        return "lower(CAST(%s AS varchar)) LIKE lower(%s) ESCAPE '!'".formatted(
                quotedIdentifier(field),
                sqlString("%" + escapeLike(value) + "%")
        );
    }

    static String quotedIdentifier(String value) {
        return "\"" + value.replace("\"", "\"\"") + "\"";
    }

    static String sqlString(String value) {
        return "'" + value.replace("'", "''") + "'";
    }

    static String escapeLike(String value) {
        return value.replace("!", "!!").replace("%", "!%").replace("_", "!_");
    }

    static String formatTimestamp(Object value) {
        if (value == null) {
            return "";
        }
        if (value instanceof Number number) {
            return Instant.ofEpochMilli(number.longValue()).atZone(JST).format(DISPLAY_TIME);
        }
        if (value instanceof String string) {
            return formatTimestampString(string);
        }
        return String.valueOf(value);
    }

    static String formatTimestampString(String value) {
        String trimmed = value.trim();
        if (trimmed.isEmpty()) {
            return "";
        }

        List<String> candidates = List.of(
                trimmed,
                trimmed.replace(" UTC", "Z"),
                trimmed.replace(" ", "T"),
                trimmed.replace(" UTC", "Z").replace(" ", "T")
        );
        for (String candidate : candidates) {
            try {
                return OffsetDateTime.parse(candidate).atZoneSameInstant(JST).format(DISPLAY_TIME);
            } catch (DateTimeParseException ignored) {
                // Try next format.
            }
            try {
                return LocalDateTime.parse(candidate).format(DISPLAY_TIME);
            } catch (DateTimeParseException ignored) {
                // Try next candidate.
            }
        }
        return value;
    }

    static Filters normalizeFilters(Map<String, String> values) {
        return new Filters(
                trim(values.get("date")),
                trim(values.get("time_from")),
                trim(values.get("time_to")),
                trim(values.get("log_type")),
                trim(values.get("host")),
                trim(values.get("program")),
                trim(values.get("message")),
                positiveInt(values.get("page"), 1),
                Math.min(positiveInt(values.get("size"), 25), 100)
        );
    }

    static Filters normalizeFilters(Filters filters) {
        return new Filters(
                trim(filters.date),
                trim(filters.timeFrom),
                trim(filters.timeTo),
                trim(filters.logType),
                trim(filters.host),
                trim(filters.program),
                trim(filters.message),
                filters.page > 0 ? filters.page : 1,
                filters.size > 0 ? Math.min(filters.size, 100) : 25
        );
    }

    static int positiveInt(String value, int fallback) {
        try {
            int parsed = Integer.parseInt(trim(value));
            return parsed > 0 ? parsed : fallback;
        } catch (NumberFormatException ex) {
            return fallback;
        }
    }

    static String trim(String value) {
        return value == null ? "" : value.trim();
    }

    private Filters filtersFromRequest(HttpExchange exchange) throws IOException {
        Optional<String> contentType = exchange.getRequestHeaders().getFirst("Content-Type") == null
                ? Optional.empty()
                : Optional.of(exchange.getRequestHeaders().getFirst("Content-Type"));
        if (contentType.orElse("").contains("application/json")) {
            try (InputStream body = exchange.getRequestBody()) {
                byte[] bytes = body.readAllBytes();
                if (bytes.length == 0) {
                    return new Filters("", "", "", "", "", "", "", 1, 25);
                }
                return normalizeFilters(JSON.readValue(bytes, Filters.class));
            }
        }
        if ("POST".equals(exchange.getRequestMethod())) {
            return normalizeFilters(parseForm(exchange));
        }
        return normalizeFilters(parseQuery(exchange.getRequestURI().getRawQuery()));
    }

    private Map<String, String> parseForm(HttpExchange exchange) throws IOException {
        String body = new String(exchange.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
        return parseQuery(body);
    }

    static Map<String, String> parseQuery(String raw) {
        Map<String, String> values = new LinkedHashMap<>();
        if (raw == null || raw.isBlank()) {
            return values;
        }
        for (String pair : raw.split("&")) {
            if (pair.isBlank()) {
                continue;
            }
            String[] parts = pair.split("=", 2);
            String key = urlDecode(parts[0]);
            String value = parts.length > 1 ? urlDecode(parts[1]) : "";
            values.put(key, value);
        }
        return values;
    }

    static String urlDecode(String value) {
        return URLDecoder.decode(value, StandardCharsets.UTF_8);
    }

    static String stringValue(Object value) {
        return value == null ? "" : String.valueOf(value);
    }

    private void sendJson(HttpExchange exchange, int status, Object payload) throws IOException {
        sendText(exchange, status, JSON.writeValueAsString(payload), "application/json; charset=utf-8");
    }

    private void sendText(HttpExchange exchange, int status, String body, String contentType) throws IOException {
        byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
        exchange.getResponseHeaders().set("Content-Type", contentType);
        exchange.sendResponseHeaders(status, bytes.length);
        try (OutputStream output = exchange.getResponseBody()) {
            output.write(bytes);
        }
    }

    interface QueryClient {
        boolean ping();
        QueryResult execute(String sql) throws Exception;
    }

    record Config(
            String port,
            String trinoUrl,
            String trinoUser,
            String trinoPassword,
            String trinoCatalog,
            String trinoSchema,
            String trinoSyslogTable,
            String trinoAuthlogTable,
            String trinoTimestampColumn,
            String trinoTimestampExpression
    ) {
        static Config fromEnv() {
            return new Config(
                    getenv("PORT", "5000"),
                    getenv("TRINO_URL", "http://trino1:8080"),
                    getenv("TRINO_USER", "log_search"),
                    System.getenv().getOrDefault("TRINO_PASSWORD", ""),
                    getenv("TRINO_CATALOG", "iceberg"),
                    getenv("TRINO_SCHEMA", "logs"),
                    getenv("TRINO_SYSLOG_TABLE", "syslog_events"),
                    getenv("TRINO_AUTHLOG_TABLE", "authlog_events"),
                    getenv("TRINO_TIMESTAMP_COLUMN", "ts"),
                    System.getenv().getOrDefault("TRINO_TIMESTAMP_EXPRESSION", "")
            );
        }
    }

    record Filters(
            String date,
            String timeFrom,
            String timeTo,
            String logType,
            String host,
            String program,
            String message,
            int page,
            int size
    ) {
    }

    record LogRecord(
            int id,
            String index,
            Object eventTime,
            String displayTime,
            String logType,
            String host,
            String program,
            String msg
    ) {
    }

    record SearchPage(List<LogRecord> logs, long total) {
    }

    record QueryResult(List<List<Object>> rows, List<String> columns) {
    }

    static String getenv(String key, String fallback) {
        String value = System.getenv(key);
        return value == null || value.isBlank() ? fallback : value;
    }

}
