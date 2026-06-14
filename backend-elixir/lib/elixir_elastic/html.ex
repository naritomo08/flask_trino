defmodule ElixirElastic.HTML do
  @moduledoc false

  alias ElixirElastic.TrinoSearch

  def render_index(filters, logs, searched, error \\ nil) do
    """
    <!doctype html>
    <html lang="ja">
      <head>
        <meta charset="utf-8">
        <meta name="viewport" content="width=device-width, initial-scale=1">
        <title>Trino Iceberg Log Search</title>
        <link rel="stylesheet" href="/static/styles.css">
      </head>
      <body>
        <main class="shell">
          <section class="header">
            <div>
              <p class="eyebrow">Trino Iceberg Log Search</p>
              <h1>当日ログ検索</h1>
              <p class="note">検索対象日はJSTの当日固定です。検索結果は条件に一致したログのうち最新50件のみ表示します。</p>
            </div>
          </section>

          #{search_form(filters)}
          #{results(logs, searched, error)}
        </main>
        <script src="/static/search.js"></script>
      </body>
    </html>
    """
  end

  defp search_form(filters) do
    """
    <form id="search-form" class="search" method="post" action="/">
      #{input("FROM (JST)", "time", "time_from", filters["time_from"])}
      #{input("TO (JST)", "time", "time_to", filters["time_to"])}
      <label>
        <span>LOG</span>
        <select name="log_type">
          <option value="">すべて</option>
          #{Enum.map_join(TrinoSearch.log_types(), "", &log_type_option(&1, filters["log_type"]))}
        </select>
      </label>
      #{input("HOST", "search", "host", filters["host"], "例: elastic1, flink1")}
      #{input("PROGRAM", "search", "program", filters["program"], "例: sshd, systemd, hdfs")}
      <label class="message-filter">
        <span>Message</span>
        <input type="search" name="message" value="#{escape(filters["message"])}" placeholder="例: accepted, JournalNodeSyncer" aria-label="Message" autofocus>
      </label>
      <div class="search-actions">
        <a class="reset-link" href="/clear">クリア</a>
        <button type="submit">検索</button>
      </div>
    </form>
    """
  end

  defp input(label, type, name, value, placeholder \\ "") do
    """
    <label>
      <span>#{label}</span>
      <input type="#{type}" name="#{name}" value="#{escape(value)}" placeholder="#{placeholder}" aria-label="#{label}">
    </label>
    """
  end

  defp log_type_option(log_type, selected) do
    selected_attr = if log_type == selected, do: " selected", else: ""
    ~s(<option value="#{log_type}"#{selected_attr}>#{log_type}</option>)
  end

  defp results(_logs, _searched, error) when is_binary(error) do
    """
    <section id="results" class="results" aria-live="polite">
      <div id="results-summary" class="summary"><span>検索エラー</span></div>
      <p id="results-body" class="empty">#{escape(error)}</p>
    </section>
    """
  end

  defp results(logs, true, nil) when logs != [] do
    """
    <section id="results" class="results" aria-live="polite">
      <div id="results-summary" class="summary">
        <span>#{length(logs)} 件</span>
        <span>最新50件のみ表示</span>
      </div>
      <div id="results-body" class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>Log</th>
              <th>Host</th>
              <th>Program</th>
              <th>Message</th>
            </tr>
          </thead>
          <tbody>
            #{Enum.map_join(logs, "", &log_row/1)}
          </tbody>
        </table>
      </div>
    </section>
    """
  end

  defp results([], true, nil) do
    """
    <section id="results" class="results" aria-live="polite">
      <div id="results-summary" class="summary">
        <span>0 件</span>
        <span>最新50件のみ表示</span>
      </div>
      <p id="results-body" class="empty">該当するログはありません。</p>
    </section>
    """
  end

  defp results([], false, nil) do
    """
    <section id="results" class="results" aria-live="polite">
      <div id="results-summary" class="summary"><span>検索を実施してください</span></div>
      <p id="results-body" class="empty">検索条件を入力して検索ボタンを押してください。</p>
    </section>
    """
  end

  defp log_row(log) do
    log_type = Map.get(log, "log_type", "unknown")

    """
    <tr>
      <td>#{escape(Map.get(log, "display_time", ""))}</td>
      <td><span class="log-type log-type-#{escape(log_type)}">#{escape(log_type)}</span></td>
      <td>#{escape(Map.get(log, "host", ""))}</td>
      <td>#{escape(Map.get(log, "program", ""))}</td>
      <td>#{escape(Map.get(log, "msg", ""))}</td>
    </tr>
    """
  end

  defp escape(nil), do: ""

  defp escape(value) do
    value
    |> to_string()
    |> String.replace("&", "&amp;")
    |> String.replace("<", "&lt;")
    |> String.replace(">", "&gt;")
    |> String.replace("\"", "&quot;")
  end
end
