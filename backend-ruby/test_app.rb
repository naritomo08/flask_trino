ENV["RACK_ENV"] = "test"

require "json"
require "minitest/autorun"
require "rack/test"
require_relative "app"

class FakeTrino
  attr_reader :queries

  def initialize
    @queries = []
  end

  def ping
    true
  end

  def execute(sql)
    @queries << sql
    [
      [["2026-06-02 20:11:55.000", "flink1", "systemd", "Reached target sshd-keygen.target.", "syslog"]],
      %w[event_time host program msg log_type]
    ]
  end
end

class LogSearchAppTest < Minitest::Test
  include Rack::Test::Methods

  def app
    LogSearchApp
  end

  def setup
    @fake_client = FakeTrino.new
    LogSearchApp.set :trino_client, @fake_client
  end

  def teardown
    LogSearchApp.set :trino_client, nil
  end

  def test_format_timestamp_converts_epoch_millis_to_jst
    assert_equal "2026/06/02 20:11:55 JST", app.new!.format_timestamp(1_780_398_715_000)
  end

  def test_format_timestamp_keeps_naive_trino_timestamp_as_jst
    assert_equal "2026/06/02 20:11:55 JST", app.new!.format_timestamp("2026-06-02 20:11:55.000")
  end

  def test_time_bound_uses_today_jst_for_time_only_input
    instance = app.new!
    def instance.today_jst
      Date.new(2026, 6, 2)
    end

    assert_equal "2026-06-02 20:11:00", instance.time_bound("20:11", "from")
    assert_equal "2026-06-02 23:59:59", instance.time_bound("", "to")
  end

  def test_escape_helpers_quote_sql_safely
    instance = app.new!

    assert_equal "'can''t'", instance.sql_string("can't")
    assert_equal '"bad""name"', instance.quoted_identifier('bad"name')
    assert_equal "100!%!!!_", instance.escape_like("100%!_")
  end

  def test_build_query_with_message_program_host_and_time_range
    instance = app.new!
    def instance.today_jst
      Date.new(2026, 6, 2)
    end

    query = instance.build_query({
      "time_from" => "20:00",
      "time_to" => "21:00",
      "log_type" => "syslog",
      "host" => "flink1",
      "program" => "systemd",
      "message" => "sshd"
    })

    assert_includes query, 'FROM "iceberg"."logs"."syslog_events"'
    refute_includes query, 'FROM "iceberg"."logs"."authlog_events"'
    assert_includes query, %("ts" >= TIMESTAMP '2026-06-02 20:00:00')
    assert_includes query, %("ts" <= TIMESTAMP '2026-06-02 21:00:00')
    assert_includes query, %(lower(CAST("host" AS varchar)) = lower('flink1'))
    assert_includes query, %(lower(CAST("program" AS varchar)) = lower('systemd'))
    assert_includes query, %(lower(CAST("message" AS varchar)) LIKE lower('%sshd%') ESCAPE '!')
    assert_includes query, "ORDER BY event_time DESC"
    assert_includes query, "LIMIT 50"
  end

  def test_build_query_searches_both_log_tables_by_default
    instance = app.new!
    query = instance.build_query(instance.normalize_filters("message" => "authlog forward test from"))

    assert_includes query, 'FROM "iceberg"."logs"."syslog_events"'
    assert_includes query, 'FROM "iceberg"."logs"."authlog_events"'
    assert_includes query, "UNION ALL"
    assert_includes query, "authlog forward test from"
  end

  def test_index_serves_static_html_client
    get "/"
    body = last_response.body.force_encoding("UTF-8")

    assert_equal 200, last_response.status
    assert_includes body, %(action="/api/logs")
    assert_includes body, %(id="search-form")
    assert_includes body, %(src="/search.js")
    assert_includes body, %(type="time")
    assert_includes body, "検索を実施してください"
    refute_includes body, "2026/06/02 20:11:55 JST"
  end

  def test_post_index_redirects_to_static_html
    post "/", { program: "systemd", message: "sshd" }
    assert_equal 302, last_response.status
    assert_equal "http://example.org/", last_response.location

    follow_redirect!
    body = last_response.body.force_encoding("UTF-8")

    assert_includes body, "検索を実施してください"
    refute_includes body, "2026/06/02 20:11:55 JST"
  end

  def test_post_api_logs_accepts_json
    post "/api/logs", JSON.generate({ program: "systemd", message: "sshd" }), "CONTENT_TYPE" => "application/json"

    assert_equal 200, last_response.status
    payload = JSON.parse(last_response.body)
    assert_equal 1, payload["count"]
    assert_equal "systemd", payload["filters"]["program"]
    assert_equal "2026/06/02 20:11:55 JST", payload["logs"][0]["display_time"]
  end
end
