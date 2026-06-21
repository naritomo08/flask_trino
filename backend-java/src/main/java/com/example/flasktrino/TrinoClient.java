package com.example.flasktrino;

import com.fasterxml.jackson.core.type.TypeReference;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Base64;
import java.util.List;
import java.util.Map;

final class TrinoClient implements App.QueryClient {
    private final App.Config config;
    private final HttpClient client = HttpClient.newBuilder()
            .connectTimeout(java.time.Duration.ofSeconds(10))
            .build();
    private final URI statementUri;

    TrinoClient(App.Config config) {
        this.config = config;
        this.statementUri = URI.create(config.trinoUrl().replaceAll("/+$", "") + "/v1/statement");
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
    public App.QueryResult execute(String sql) throws Exception {
        HttpRequest.Builder builder = HttpRequest.newBuilder(statementUri)
                .timeout(java.time.Duration.ofSeconds(60))
                .POST(HttpRequest.BodyPublishers.ofString(sql, StandardCharsets.UTF_8));
        applyHeaders(builder);
        HttpResponse<String> response = client.send(
                builder.build(),
                HttpResponse.BodyHandlers.ofString(StandardCharsets.UTF_8)
        );
        if (response.statusCode() < 200 || response.statusCode() >= 300) {
            throw new IOException("trino statement failed: HTTP " + response.statusCode() + ": " + response.body());
        }
        return collect(App.JSON.readValue(response.body(), new TypeReference<>() {
        }));
    }

    private App.QueryResult collect(Map<String, Object> page) throws Exception {
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
                return new App.QueryResult(rows, columns);
            }

            HttpRequest.Builder builder = HttpRequest.newBuilder(URI.create(String.valueOf(nextUri)))
                    .timeout(java.time.Duration.ofSeconds(60))
                    .GET();
            applyHeaders(builder);
            HttpResponse<String> response = client.send(
                    builder.build(),
                    HttpResponse.BodyHandlers.ofString(StandardCharsets.UTF_8)
            );
            if (response.statusCode() < 200 || response.statusCode() >= 300) {
                throw new IOException("trino next page failed: HTTP " + response.statusCode() + ": " + response.body());
            }
            page = App.JSON.readValue(response.body(), new TypeReference<>() {
            });
        }
    }

    private void applyHeaders(HttpRequest.Builder builder) {
        builder.header("X-Trino-User", config.trinoUser())
                .header("X-Trino-Source", "java-trino-log-search")
                .header("Content-Type", "text/plain; charset=utf-8");
        if (!config.trinoCatalog().isBlank()) {
            builder.header("X-Trino-Catalog", config.trinoCatalog());
        }
        if (!config.trinoSchema().isBlank()) {
            builder.header("X-Trino-Schema", config.trinoSchema());
        }
        if (!config.trinoPassword().isBlank()) {
            String token = Base64.getEncoder().encodeToString(
                    (config.trinoUser() + ":" + config.trinoPassword()).getBytes(StandardCharsets.UTF_8)
            );
            builder.header("Authorization", "Basic " + token);
        }
    }
}
