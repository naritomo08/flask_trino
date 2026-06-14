defmodule ElixirElastic.TrinoSearch do
  @moduledoc false

  @log_types ["syslog", "authlog"]
  @jst_offset_seconds 9 * 60 * 60
  @default_limit 50

  def log_types, do: @log_types

  def ping do
    sql = "SELECT 1"

    case execute(sql, receive_timeout: 5_000) do
      {:ok, _rows, _columns} -> true
      _ -> false
    end
  end

  def search_logs(filters) do
    sql = build_query(filters)

    case execute(sql, receive_timeout: 15_000) do
      {:ok, rows, columns} ->
        Enum.map(rows, &format_row(&1, columns))

      {:error, reason} ->
        raise "Trino search failed: #{reason}"
    end
  end

  def build_query(filters) do
    filters
    |> target_log_types()
    |> Enum.map(&select_for_log_type(&1, filters))
    |> Enum.join("\nUNION ALL\n")
    |> then(fn sql ->
      "SELECT * FROM (\n#{sql}\n) logs\nORDER BY event_time DESC\nLIMIT #{@default_limit}"
    end)
  end

  def today_jst do
    DateTime.utc_now()
    |> DateTime.add(@jst_offset_seconds, :second)
    |> DateTime.to_date()
  end

  def time_bound("", :from, date), do: "#{Date.to_iso8601(date)} 00:00:00"
  def time_bound(nil, :from, date), do: "#{Date.to_iso8601(date)} 00:00:00"
  def time_bound("", :to, date), do: "#{Date.to_iso8601(date)} 23:59:59"
  def time_bound(nil, :to, date), do: "#{Date.to_iso8601(date)} 23:59:59"

  def time_bound(value, _direction, date) do
    value = to_string(value)

    case Time.from_iso8601(add_seconds(value)) do
      {:ok, time} ->
        "#{Date.to_iso8601(date)} #{Calendar.strftime(time, "%H:%M:%S")}"

      {:error, _reason} ->
        "#{Date.to_iso8601(date)} 00:00:00"
    end
  end

  def format_timestamp(nil), do: ""

  def format_timestamp(%NaiveDateTime{} = value) do
    Calendar.strftime(value, "%Y/%m/%d %H:%M:%S JST")
  end

  def format_timestamp(value) when is_binary(value) do
    value
    |> String.trim()
    |> parse_timestamp()
  end

  def format_timestamp(value), do: to_string(value)

  defp select_for_log_type(log_type, filters) do
    date = today_jst()
    table = table_for_log_type(log_type)
    timestamp_sql = timestamp_expression_sql()
    from = time_bound(filters["time_from"], :from, date)
    to = time_bound(filters["time_to"], :to, date)

    conditions =
      [
        "#{timestamp_sql} >= TIMESTAMP #{sql_string(from)}",
        "#{timestamp_sql} <= TIMESTAMP #{sql_string(to)}"
      ]
      |> append_equals(filters["host"], "host")
      |> append_equals(filters["program"], "program")
      |> append_like(filters["message"], "message")

    """
    SELECT
      #{timestamp_sql} AS event_time,
      CAST(#{quoted_identifier("host")} AS varchar) AS host,
      CAST(#{quoted_identifier("program")} AS varchar) AS program,
      CAST(#{quoted_identifier("message")} AS varchar) AS msg,
      #{sql_string(log_type)} AS log_type
    FROM #{table}
    WHERE #{Enum.join(conditions, " AND ")}
    """
  end

  defp execute(sql, opts) do
    headers =
      [
        {"x-trino-user", trino_user()},
        {"x-trino-source", "elixir-elastic"},
        {"x-trino-catalog", trino_catalog()},
        {"x-trino-schema", trino_schema()}
      ]
      |> Enum.reject(fn {_key, value} -> value in [nil, ""] end)

    request_opts =
      opts
      |> Keyword.put(:body, sql)
      |> Keyword.put(:headers, headers)
      |> maybe_auth()

    case Req.post(statement_url(), request_opts) do
      {:ok, %{status: status, body: body}} when status in 200..299 ->
        collect_pages(body, [], [])

      {:ok, %{status: status, body: body}} ->
        {:error, "HTTP #{status}: #{inspect(body)}"}

      {:error, reason} ->
        {:error, inspect(reason)}
    end
  end

  defp collect_pages(%{"error" => error}, _rows, _columns) do
    {:error, Map.get(error, "message", inspect(error))}
  end

  defp collect_pages(body, rows, columns) do
    rows = rows ++ Map.get(body, "data", [])
    columns = columns_for(body, columns)

    case Map.get(body, "nextUri") do
      nil ->
        {:ok, rows, columns}

      next_uri ->
        case Req.get(next_uri, maybe_auth(receive_timeout: 15_000)) do
          {:ok, %{status: status, body: next_body}} when status in 200..299 ->
            collect_pages(next_body, rows, columns)

          {:ok, %{status: status, body: next_body}} ->
            {:error, "HTTP #{status}: #{inspect(next_body)}"}

          {:error, reason} ->
            {:error, inspect(reason)}
        end
    end
  end

  defp columns_for(%{"columns" => columns}, []) do
    Enum.map(columns, &Map.fetch!(&1, "name"))
  end

  defp columns_for(_body, columns), do: columns

  defp format_row(row, columns) do
    columns
    |> Enum.zip(row)
    |> Map.new()
    |> Map.update("event_time", "", &format_timestamp/1)
    |> Map.put_new("display_time", "")
    |> then(fn log -> Map.put(log, "display_time", Map.get(log, "event_time", "")) end)
  end

  defp append_like(conditions, nil, _field), do: conditions
  defp append_like(conditions, "", _field), do: conditions

  defp append_like(conditions, value, field) do
    conditions ++
      [
        "lower(CAST(#{quoted_identifier(field)} AS varchar)) LIKE lower(#{sql_string("%#{escape_like(value)}%")}) ESCAPE '!'"
      ]
  end

  defp append_equals(conditions, nil, _field), do: conditions
  defp append_equals(conditions, "", _field), do: conditions

  defp append_equals(conditions, value, field) do
    conditions ++
      [
        "lower(CAST(#{quoted_identifier(field)} AS varchar)) = lower(#{sql_string(value)})"
      ]
  end

  defp target_log_types(%{"log_type" => log_type}) when log_type in @log_types, do: [log_type]
  defp target_log_types(_filters), do: @log_types

  defp table_for_log_type("syslog"), do: table_expr(syslog_table())
  defp table_for_log_type("authlog"), do: table_expr(authlog_table())

  defp table_expr(name) do
    parts = String.split(name, ".", trim: true)

    parts =
      if length(parts) == 1 do
        [trino_catalog(), trino_schema(), name] |> Enum.reject(&(&1 in [nil, ""]))
      else
        parts
      end

    Enum.map_join(parts, ".", &quoted_identifier/1)
  end

  defp timestamp_expression_sql do
    case timestamp_expression() do
      "" -> quoted_identifier(timestamp_column())
      expression -> expression
    end
  end

  defp quoted_identifier(value) do
    ~s("#{String.replace(to_string(value), "\"", "\"\"")}")
  end

  defp sql_string(value) do
    "'#{String.replace(to_string(value), "'", "''")}'"
  end

  defp escape_like(value) do
    value
    |> to_string()
    |> String.replace("!", "!!")
    |> String.replace("%", "!%")
    |> String.replace("_", "!_")
  end

  defp parse_timestamp(value) do
    normalized =
      value
      |> String.replace(" UTC", "Z")
      |> String.replace(" ", "T")

    cond do
      match?({:ok, _, _}, DateTime.from_iso8601(normalized)) ->
        {:ok, datetime, _offset} = DateTime.from_iso8601(normalized)

        datetime
        |> DateTime.add(@jst_offset_seconds, :second)
        |> Calendar.strftime("%Y/%m/%d %H:%M:%S JST")

      match?({:ok, _}, NaiveDateTime.from_iso8601(normalized)) ->
        {:ok, naive} = NaiveDateTime.from_iso8601(normalized)
        Calendar.strftime(naive, "%Y/%m/%d %H:%M:%S JST")

      true ->
        value
    end
  end

  defp add_seconds(value) do
    if String.length(value) == 5, do: value <> ":00", else: value
  end

  defp maybe_auth(opts) do
    password = trino_password()

    if password == "" do
      opts
    else
      Keyword.put(opts, :auth, {:basic, trino_user() <> ":" <> password})
    end
  end

  defp statement_url, do: "#{trino_url()}/v1/statement"
  defp trino_url, do: Application.fetch_env!(:elixir_elastic, :trino_url)
  defp trino_user, do: Application.fetch_env!(:elixir_elastic, :trino_user)
  defp trino_password, do: Application.fetch_env!(:elixir_elastic, :trino_password)
  defp trino_catalog, do: Application.fetch_env!(:elixir_elastic, :trino_catalog)
  defp trino_schema, do: Application.fetch_env!(:elixir_elastic, :trino_schema)
  defp syslog_table, do: Application.fetch_env!(:elixir_elastic, :syslog_table)
  defp authlog_table, do: Application.fetch_env!(:elixir_elastic, :authlog_table)
  defp timestamp_column, do: Application.fetch_env!(:elixir_elastic, :timestamp_column)
  defp timestamp_expression, do: Application.fetch_env!(:elixir_elastic, :timestamp_expression)
end
