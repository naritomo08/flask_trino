package com.example.flasktrino;

import com.fasterxml.jackson.core.type.TypeReference;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.PropertyNamingStrategies;
import com.sun.net.httpserver.Headers;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpHandler;
import com.sun.net.httpserver.HttpServer;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.io.UncheckedIOException;
import java.net.InetSocketAddress;
import java.net.URI;
import java.net.URLDecoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Clock;
import java.time.Instant;
import java.time.LocalDate;
import java.time.LocalDateTime;
import java.time.LocalTime;
import java.time.OffsetDateTime;
import java.time.ZoneId;
import java.time.ZoneOffset;
import java.time.format.DateTimeFormatter;
import java.time.format.DateTimeParseException;
import java.util.ArrayList;
import java.util.Base64;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Objects;
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
    private final String indexTemplate;

    public App(Config config, QueryClient queryClient, Clock clock) {
        this.config = config;
        this.queryClient = queryClient;
        this.clock = clock;
        this.indexTemplate = loadResource("/index.html");
    }

    static String loadResource(String path) {
        try (InputStream is = App.class.getResourceAsStream(path)) {
            if (is == null) throw new IllegalStateException("resource not found: " + path);
            return new String(is.readAllBytes(), StandardCharsets.UTF_8);
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
    }

    public static void main(String[] args) throws IOException {
        Config config = Config.fromEnv();
        App app = new App(config, new TrinoClient(config), Clock.systemUTC());
        app.start();
    }

    void start() throws IOException {
        HttpServer server = HttpServer.create(new InetSocketAddress(Integer.parseInt(config.port)), 0);
        server.createContext("/", this::handleIndex);
        server.createContext("/clear", this::handleClear);
        server.createContext("/health", this::handleHealth);
        server.createContext("/api/logs", this::handleApiLogs);
        server.createContext("/static", new StaticHandler(Path.of(config.staticDir)));
        server.setExecutor(Executors.newFixedThreadPool(16));
        server.start();
        System.out.printf("listening on :%s%n", config.port);
    }

    private void handleIndex(HttpExchange exchange) throws IOException {
        String method = exchange.getRequestMethod();
        if ("POST".equals(method)) {
            Filters filters = normalizeFilters(parseForm(exchange));
            setSearchCookie(exchange, filters);
            redirect(exchange, "/");
            return;
        }
        if (!"GET".equals(method)) {
            sendText(exchange, 405, "method not allowed", "text/plain; charset=utf-8");
            return;
        }

        CookieSearch cookieSearch = popSearchCookie(exchange);
        Filters filters = cookieSearch.filters;
        boolean searched = cookieSearch.searched;
        Map<String, String> query = parseQuery(exchange.getRequestURI().getRawQuery());
        if (!query.isEmpty()) {
            filters = normalizeFilters(query);
            searched = true;
        }

        List<LogRecord> logs = List.of();
        String error = "";
        if (searched) {
            try {
                logs = searchLogs(queryClient, config, filters, clock);
            } catch (Exception ex) {
                error = ex.getMessage();
            }
        }

        String html = renderIndex(filters, logs, searched, error);
        sendText(exchange, 200, html, "text/html; charset=utf-8");
    }

    private void handleClear(HttpExchange exchange) throws IOException {
        clearSearchCookie(exchange);
        redirect(exchange, "/");
    }

    private void handleHealth(HttpExchange exchange) throws IOException {
        Map<String, Object> payload = new LinkedHashMap<>();
        payload.put("ok", queryClient.ping());
        payload.put("trino_url", config.trinoUrl);
        payload.put("catalog", config.trinoCatalog);
        payload.put("schema", config.trinoSchema);
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
            List<LogRecord> logs = searchLogs(queryClient, config, filters, clock);
            Map<String, Object> payload = new LinkedHashMap<>();
            payload.put("filters", filters);
            payload.put("count", logs.size());
            payload.put("logs", logs);
            sendJson(exchange, 200, payload);
        } catch (Exception ex) {
            sendText(exchange, 502, ex.getMessage(), "text/plain; charset=utf-8");
        }
    }

    static List<LogRecord> searchLogs(QueryClient client, Config config, Filters filters, Clock clock) throws Exception {
        QueryResult result = client.execute(buildQuery(config, filters, clock));
        List<LogRecord> logs = new ArrayList<>();
        for (int i = 0; i < result.rows.size(); i++) {
            Map<String, Object> row = new LinkedHashMap<>();
            List<Object> values = result.rows.get(i);
            for (int j = 0; j < result.columns.size() && j < values.size(); j++) {
                row.put(result.columns.get(j), values.get(j));
            }
            logs.add(new LogRecord(
                    i,
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

    static String buildQuery(Config config, Filters filters, Clock clock) {
        List<String> selects = new ArrayList<>();
        for (String logType : targetLogTypes(filters)) {
            selects.add(selectForLogType(config, filters, logType, clock));
        }
        return "SELECT * FROM (\n"
                + String.join("\nUNION ALL\n", selects)
                + "\n) logs\nORDER BY event_time DESC\nLIMIT "
                + config.trinoLimit;
    }

    static String selectForLogType(Config config, Filters filters, String logType, Clock clock) {
        String timestampSql = timestampExpressionSql(config);
        List<String> conditions = new ArrayList<>();
        conditions.add(timestampSql + " >= TIMESTAMP " + sqlString(timeBound(filters.timeFrom, "from", clock)));
        conditions.add(timestampSql + " <= TIMESTAMP " + sqlString(timeBound(filters.timeTo, "to", clock)));
        if (!filters.host.isBlank()) {
            conditions.add(equalsCondition("host", filters.host));
        }
        if (!filters.program.isBlank()) {
            conditions.add(equalsCondition("program", filters.program));
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
        LocalDate today = LocalDate.now(clock.withZone(JST));
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
                trim(values.get("time_from")),
                trim(values.get("time_to")),
                trim(values.get("log_type")),
                trim(values.get("host")),
                trim(values.get("program")),
                trim(values.get("message"))
        );
    }

    static Filters normalizeFilters(Filters filters) {
        return new Filters(
                trim(filters.timeFrom),
                trim(filters.timeTo),
                trim(filters.logType),
                trim(filters.host),
                trim(filters.program),
                trim(filters.message)
        );
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
                    return new Filters("", "", "", "", "", "");
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

    private void setSearchCookie(HttpExchange exchange, Filters filters) throws IOException {
        String payload = Base64.getUrlEncoder().withoutPadding().encodeToString(JSON.writeValueAsBytes(filters));
        exchange.getResponseHeaders().add("Set-Cookie", config.sessionCookieName + "=" + payload + "; Path=/; Max-Age=60; HttpOnly; SameSite=Lax");
    }

    private CookieSearch popSearchCookie(HttpExchange exchange) throws IOException {
        String cookie = exchange.getRequestHeaders().getFirst("Cookie");
        if (cookie == null || cookie.isBlank()) {
            return new CookieSearch(new Filters("", "", "", "", "", ""), false);
        }
        clearSearchCookie(exchange);
        String prefix = config.sessionCookieName + "=";
        for (String part : cookie.split(";")) {
            String trimmed = part.trim();
            if (trimmed.startsWith(prefix)) {
                byte[] decoded = Base64.getUrlDecoder().decode(trimmed.substring(prefix.length()));
                return new CookieSearch(normalizeFilters(JSON.readValue(decoded, Filters.class)), true);
            }
        }
        return new CookieSearch(new Filters("", "", "", "", "", ""), false);
    }

    private void clearSearchCookie(HttpExchange exchange) {
        exchange.getResponseHeaders().add("Set-Cookie", config.sessionCookieName + "=; Path=/; Max-Age=0; HttpOnly; SameSite=Lax");
    }

    private String renderIndex(Filters filters, List<LogRecord> logs, boolean searched, String error) {
        StringBuilder options = new StringBuilder();
        for (String logType : LOG_TYPES) {
            String selected = Objects.equals(filters.logType, logType) ? " selected" : "";
            options.append("<option value=\"").append(escapeHtml(logType)).append("\"").append(selected)
                    .append(">").append(escapeHtml(logType)).append("</option>");
        }

        String summary = searched
                ? "<span>" + logs.size() + " 件</span><span>最新50件のみ表示</span>"
                : "<span>検索を実施してください</span>";

        String body;
        if (!error.isBlank()) {
            body = "<p id=\"results-body\" class=\"empty\">" + escapeHtml(error) + "</p>";
        } else if (!searched) {
            body = "<p id=\"results-body\" class=\"empty\">検索条件を入力して検索ボタンを押してください。</p>";
        } else if (logs.isEmpty()) {
            body = "<p id=\"results-body\" class=\"empty\">該当するログはありません。</p>";
        } else {
            StringBuilder table = new StringBuilder();
            table.append("<div id=\"results-body\" class=\"table-wrap\"><table><thead><tr>")
                    .append("<th>Time</th><th>Log</th><th>Host</th><th>Program</th><th>Message</th>")
                    .append("</tr></thead><tbody>");
            for (LogRecord log : logs) {
                table.append("<tr><td>").append(escapeHtml(log.displayTime()))
                        .append("</td><td><span class=\"log-type log-type-").append(escapeHtml(log.logType()))
                        .append("\">").append(escapeHtml(log.logType())).append("</span></td><td>")
                        .append(escapeHtml(log.host())).append("</td><td>").append(escapeHtml(log.program()))
                        .append("</td><td>").append(escapeHtml(log.msg())).append("</td></tr>");
            }
            table.append("</tbody></table></div>");
            body = table.toString();
        }

        return indexTemplate
                .replace("{{timeFrom}}", escapeHtml(filters.timeFrom))
                .replace("{{timeTo}}", escapeHtml(filters.timeTo))
                .replace("{{logTypeOptions}}", options.toString())
                .replace("{{host}}", escapeHtml(filters.host))
                .replace("{{program}}", escapeHtml(filters.program))
                .replace("{{message}}", escapeHtml(filters.message))
                .replace("{{resultsSummary}}", summary)
                .replace("{{resultsBody}}", body);
    }

    static String escapeHtml(String value) {
        return value == null ? "" : value
                .replace("&", "&amp;")
                .replace("<", "&lt;")
                .replace(">", "&gt;")
                .replace("\"", "&quot;")
                .replace("'", "&#39;");
    }

    static String stringValue(Object value) {
        return value == null ? "" : String.valueOf(value);
    }

    private void redirect(HttpExchange exchange, String location) throws IOException {
        exchange.getResponseHeaders().add("Location", location);
        exchange.sendResponseHeaders(303, -1);
        exchange.close();
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
            String trinoTimestampExpression,
            int trinoLimit,
            String staticDir,
            String sessionCookieName
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
                    System.getenv().getOrDefault("TRINO_TIMESTAMP_EXPRESSION", ""),
                    getenvInt("TRINO_LIMIT", 50),
                    getenv("STATIC_DIR", "static"),
                    "java_log_search_filters"
            );
        }
    }

    record Filters(
            String timeFrom,
            String timeTo,
            String logType,
            String host,
            String program,
            String message
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

    record QueryResult(List<List<Object>> rows, List<String> columns) {
    }

    record CookieSearch(Filters filters, boolean searched) {
    }

    static class TrinoClient implements QueryClient {
        private final Config config;
        private final HttpClient client = HttpClient.newBuilder().connectTimeout(java.time.Duration.ofSeconds(10)).build();
        private final URI statementUri;

        TrinoClient(Config config) {
            this.config = config;
            this.statementUri = URI.create(config.trinoUrl.replaceAll("/+$", "") + "/v1/statement");
        }

        @Override
        public boolean ping() {
            try {
                execute("SELECT 1");
                return true;
            } catch (Exception ignored) {
                return false;
            }
        }

        @Override
        public QueryResult execute(String sql) throws Exception {
            HttpRequest.Builder builder = HttpRequest.newBuilder(statementUri)
                    .timeout(java.time.Duration.ofSeconds(15))
                    .POST(HttpRequest.BodyPublishers.ofString(sql, StandardCharsets.UTF_8));
            applyHeaders(builder);
            HttpResponse<String> response = client.send(builder.build(), HttpResponse.BodyHandlers.ofString(StandardCharsets.UTF_8));
            if (response.statusCode() < 200 || response.statusCode() >= 300) {
                throw new IOException("trino statement failed: HTTP " + response.statusCode() + ": " + response.body());
            }
            return collect(JSON.readValue(response.body(), new TypeReference<>() {
            }));
        }

        private QueryResult collect(Map<String, Object> page) throws Exception {
            List<List<Object>> rows = new ArrayList<>();
            List<String> columns = new ArrayList<>();

            while (true) {
                if (page.containsKey("error")) {
                    Map<?, ?> error = (Map<?, ?>) page.get("error");
                    Object message = error.get("message");
                    throw new IOException(message == null ? String.valueOf(error) : String.valueOf(message));
                }
                Object data = page.get("data");
                if (data instanceof List<?> dataRows) {
                    for (Object dataRow : dataRows) {
                        rows.add(new ArrayList<>((List<Object>) dataRow));
                    }
                }
                if (columns.isEmpty() && page.get("columns") instanceof List<?> columnRows) {
                    for (Object columnRow : columnRows) {
                        Map<?, ?> column = (Map<?, ?>) columnRow;
                        columns.add(String.valueOf(column.get("name")));
                    }
                }
                Object nextUri = page.get("nextUri");
                if (nextUri == null || String.valueOf(nextUri).isBlank()) {
                    return new QueryResult(rows, columns);
                }

                HttpRequest.Builder builder = HttpRequest.newBuilder(URI.create(String.valueOf(nextUri)))
                        .timeout(java.time.Duration.ofSeconds(15))
                        .GET();
                applyHeaders(builder);
                HttpResponse<String> response = client.send(builder.build(), HttpResponse.BodyHandlers.ofString(StandardCharsets.UTF_8));
                if (response.statusCode() < 200 || response.statusCode() >= 300) {
                    throw new IOException("trino next page failed: HTTP " + response.statusCode() + ": " + response.body());
                }
                page = JSON.readValue(response.body(), new TypeReference<>() {
                });
            }
        }

        private void applyHeaders(HttpRequest.Builder builder) {
            builder.header("X-Trino-User", config.trinoUser)
                    .header("X-Trino-Source", "java-trino-log-search")
                    .header("Content-Type", "text/plain; charset=utf-8");
            if (!config.trinoCatalog.isBlank()) {
                builder.header("X-Trino-Catalog", config.trinoCatalog);
            }
            if (!config.trinoSchema.isBlank()) {
                builder.header("X-Trino-Schema", config.trinoSchema);
            }
            if (!config.trinoPassword.isBlank()) {
                String token = Base64.getEncoder().encodeToString((config.trinoUser + ":" + config.trinoPassword).getBytes(StandardCharsets.UTF_8));
                builder.header("Authorization", "Basic " + token);
            }
        }
    }

    static class StaticHandler implements HttpHandler {
        private final Path staticDir;

        StaticHandler(Path staticDir) {
            this.staticDir = staticDir;
        }

        @Override
        public void handle(HttpExchange exchange) throws IOException {
            String rawPath = exchange.getRequestURI().getPath().replaceFirst("^/static/?", "");
            Path file = staticDir.resolve(rawPath).normalize();
            if (!file.startsWith(staticDir.normalize()) || !Files.isRegularFile(file)) {
                byte[] notFound = "not found".getBytes(StandardCharsets.UTF_8);
                exchange.sendResponseHeaders(404, notFound.length);
                try (OutputStream output = exchange.getResponseBody()) {
                    output.write(notFound);
                }
                return;
            }
            Headers headers = exchange.getResponseHeaders();
            if (file.toString().endsWith(".css")) {
                headers.set("Content-Type", "text/css; charset=utf-8");
            } else if (file.toString().endsWith(".js")) {
                headers.set("Content-Type", "application/javascript; charset=utf-8");
            }
            byte[] bytes = Files.readAllBytes(file);
            exchange.sendResponseHeaders(200, bytes.length);
            try (OutputStream output = exchange.getResponseBody()) {
                output.write(bytes);
            }
        }
    }

    static String getenv(String key, String fallback) {
        String value = System.getenv(key);
        return value == null || value.isBlank() ? fallback : value;
    }

    static int getenvInt(String key, int fallback) {
        try {
            return Integer.parseInt(System.getenv().getOrDefault(key, ""));
        } catch (NumberFormatException ex) {
            return fallback;
        }
    }
}
