defmodule ElixirElastic.TrinoSearchTest do
  use ExUnit.Case, async: true

  alias ElixirElastic.TrinoSearch

  setup do
    Application.put_env(:elixir_elastic, :trino_url, "http://trino1:8080")
    Application.put_env(:elixir_elastic, :trino_user, "log_search")
    Application.put_env(:elixir_elastic, :trino_password, "")
    Application.put_env(:elixir_elastic, :trino_catalog, "iceberg")
    Application.put_env(:elixir_elastic, :trino_schema, "logs")
    Application.put_env(:elixir_elastic, :syslog_table, "syslog_events")
    Application.put_env(:elixir_elastic, :authlog_table, "authlog_events")
    Application.put_env(:elixir_elastic, :timestamp_column, "ts")
    Application.put_env(:elixir_elastic, :timestamp_expression, "")
    Application.put_env(:elixir_elastic, :trino_limit, 25)

    :ok
  end

  test "time_bound uses direction-specific fallback" do
    date = ~D[2026-06-02]

    assert TrinoSearch.time_bound("20:11", :from, date) == "2026-06-02 20:11:00"
    assert TrinoSearch.time_bound("", :to, date) == "2026-06-02 23:59:59"
    assert TrinoSearch.time_bound("invalid", :to, date) == "2026-06-02 23:59:59"
  end

  test "build_query uses configured limit" do
    query =
      TrinoSearch.build_query(%{
        "time_from" => "",
        "time_to" => "",
        "log_type" => "syslog",
        "host" => "",
        "program" => "",
        "message" => "sshd"
      })

    assert query =~ ~s(FROM "iceberg"."logs"."syslog_events")
    refute query =~ ~s(FROM "iceberg"."logs"."authlog_events")
    assert query =~ "LIMIT 25"
  end
end
