defmodule ElixirElastic.TrinoSearch do
  @moduledoc false

  alias ElixirElastic.QueryBuilder
  alias ElixirElastic.TrinoClient

  @log_types ["syslog", "authlog"]
  @jst_offset_seconds 9 * 60 * 60

  def log_types, do: @log_types
  def build_query(filters), do: QueryBuilder.build_query(filters)
  def build_count_query(filters), do: QueryBuilder.build_count_query(filters)
  def today_jst, do: QueryBuilder.today_jst()
  def time_bound(value, direction, date), do: QueryBuilder.time_bound(value, direction, date)

  def ping do
    case TrinoClient.execute("SELECT 1", receive_timeout: 5_000) do
      {:ok, _rows, _columns} -> true
      _ -> false
    end
  end

  def search_logs(filters) do
    case TrinoClient.execute(build_query(filters), receive_timeout: 15_000) do
      {:ok, rows, columns} ->
        rows
        |> Enum.with_index((filters["page"] - 1) * filters["size"])
        |> Enum.map(fn {row, index} -> format_row(row, columns, index) end)

      {:error, reason} ->
        raise "Trino search failed: #{reason}"
    end
  end

  def search_logs_page(filters) do
    {rows, columns} =
      case TrinoClient.execute(build_query(filters), receive_timeout: 60_000) do
        {:ok, rows, columns} -> {rows, columns}
        {:error, reason} -> raise "Trino search failed: #{reason}"
      end

    total_index = Enum.find_index(columns, &(&1 == "total_count"))
    total = if total_index && rows != [], do: rows |> hd() |> Enum.at(total_index) |> to_integer(), else: 0
    offset = (filters["page"] - 1) * filters["size"]
    logs = rows |> Enum.with_index(offset) |> Enum.map(fn {row, index} -> format_row(row, columns, index) end)
    size = filters["size"]

    %{
      count: length(logs),
      total: total,
      page: filters["page"],
      size: size,
      total_pages: max(ceil(total / size), 1),
      logs: logs
    }
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

  defp format_row(row, columns, index) do
    columns
    |> Enum.zip(row)
    |> Map.new()
    |> Map.update("event_time", "", &format_timestamp/1)
    |> Map.delete("total_count")
    |> Map.put_new("display_time", "")
    |> then(fn log -> Map.put(log, "display_time", Map.get(log, "event_time", "")) end)
    |> Map.put("id", index)
    |> Map.put("index", "#{trino_catalog()}.#{trino_schema()}")
  end

  defp to_integer(value) when is_integer(value), do: value
  defp to_integer(value) when is_float(value), do: trunc(value)

  defp to_integer(value) do
    case Integer.parse(to_string(value)) do
      {number, _} -> number
      _ -> 0
    end
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

  defp trino_catalog, do: Application.fetch_env!(:elixir_elastic, :trino_catalog)
  defp trino_schema, do: Application.fetch_env!(:elixir_elastic, :trino_schema)
end
