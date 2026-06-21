defmodule ElixirElastic.TrinoClient do
  @moduledoc false

  def execute(sql, opts) do
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
        case Req.get(next_uri, maybe_auth(receive_timeout: 60_000)) do
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

  defp maybe_auth(opts) do
    if trino_password() == "" do
      opts
    else
      Keyword.put(opts, :auth, {:basic, trino_user() <> ":" <> trino_password()})
    end
  end

  defp statement_url, do: "#{String.trim_trailing(trino_url(), "/")}/v1/statement"
  defp trino_url, do: Application.fetch_env!(:elixir_elastic, :trino_url)
  defp trino_user, do: Application.fetch_env!(:elixir_elastic, :trino_user)
  defp trino_password, do: Application.fetch_env!(:elixir_elastic, :trino_password)
  defp trino_catalog, do: Application.fetch_env!(:elixir_elastic, :trino_catalog)
  defp trino_schema, do: Application.fetch_env!(:elixir_elastic, :trino_schema)
end
