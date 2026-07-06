import { escapeHtml, highlight } from "./utils.js";

export function searchForm(params, className = "") {
  return `
    <form class="search-form ${className}" action="/search" method="get" data-search-form>
      <div class="primary-search">
        <span class="search-icon" aria-hidden="true"></span>
        <input
          type="search"
          name="message"
          value="${escapeHtml(params.message)}"
          placeholder="メッセージを検索（例: timeout, accepted）"
          aria-label="ログメッセージ"
        >
        <button type="submit">検索</button>
      </div>
      <details class="filters">
        <summary>詳細条件 <span>時刻はJSTです</span></summary>
        <div class="filter-grid">
          ${field("date", "対象日", `<input type="date" name="date" value="${escapeHtml(params.date)}">`)}
          ${field("time_from", "開始時刻", `<input type="time" name="time_from" value="${escapeHtml(params.time_from)}">`)}
          ${field("time_to", "終了時刻", `<input type="time" name="time_to" value="${escapeHtml(params.time_to)}">`)}
          ${field("log_type", "ログ種別", `
            <select name="log_type">
              <option value="">すべて</option>
              <option value="syslog"${params.log_type === "syslog" ? " selected" : ""}>syslog</option>
              <option value="authlog"${params.log_type === "authlog" ? " selected" : ""}>authlog</option>
            </select>`)}
          ${field("host", "ホスト", `<input type="search" name="host" value="${escapeHtml(params.host)}" placeholder="elastic1 または /^web\\d+$/">`)}
          ${field("program", "プログラム", `<input type="search" name="program" value="${escapeHtml(params.program)}" placeholder="sshd または /^ssh.*/">`)}
          ${field("size", "表示件数", `
            <select name="size">
              ${[10, 25, 50, 100].map((size) => `<option value="${size}"${Number(params.size) === size ? " selected" : ""}>${size}件</option>`).join("")}
            </select>`)}
        </div>
        <div class="filter-actions">
          <button class="button-ghost" type="reset">すべての条件をクリア</button>
        </div>
      </details>
    </form>
  `;
}

export function homeLogsMarkup(logs) {
  return logs.length
    ? `<div class="log-grid">${logs.map(homeLogCard).join("")}</div>`
    : emptyState("ログがありません", "対象日のログがまだ登録されていません。");
}

export function resultCard(log, index, keyword, currentParams = "") {
  return `
    <article class="result-card">
      <div class="result-card-top">
        <div class="log-meta">
          <time>${escapeHtml(log.display_time || log.event_time || "—")}</time>
          ${resultFilterLink("log_type", log.log_type, log.log_type || "unknown", `log-type ${log.log_type || "unknown"}`, currentParams)}
        </div>
        <span class="index-name">${escapeHtml(log.index || "")}</span>
      </div>
      <div class="result-identity">
        ${resultFilterLink("host", log.host, log.host || "unknown host", "source-filter host-filter", currentParams)}
        <span>/</span>
        ${resultFilterLink("program", log.program, log.program || "unknown program", "source-filter", currentParams)}
      </div>
      <p class="result-message">${highlight(log.msg || "", keyword)}</p>
      <button class="card-link" type="button" data-log-index="${index}">すべてのフィールドを表示 <span>→</span></button>
    </article>
  `;
}

export function pagination(page, totalPages, currentParams = "") {
  if (totalPages <= 1) return "";
  const pageLink = (target, label) => {
    const params = new URLSearchParams(currentParams);
    params.set("page", target);
    return `<a href="/search?${escapeHtml(params.toString())}" data-route>${label}</a>`;
  };
  return `
    <nav class="pagination" aria-label="検索結果ページ">
      ${page > 1 ? pageLink(page - 1, "← 前へ") : `<span class="disabled">← 前へ</span>`}
      <span>${page} / ${totalPages}</span>
      ${page < totalPages ? pageLink(page + 1, "次へ →") : `<span class="disabled">次へ →</span>`}
    </nav>
  `;
}

export function showLogDetail(log) {
  if (!log) return;
  const logDetail = document.querySelector("#log-detail");
  const logDialog = document.querySelector("#log-dialog");
  logDetail.innerHTML = `
    <dl class="detail-list">
      ${detailRow("時刻", log.display_time || log.event_time)}
      ${detailRow("ログ種別", log.log_type)}
      ${detailRow("ホスト", log.host)}
      ${detailRow("プログラム", log.program)}
      ${detailRow("メッセージ", log.msg, true)}
      ${detailRow("データソース", log.index)}
    </dl>
  `;
  logDialog.showModal();
}

export function emptyState(title, description) {
  return `<div class="empty-state"><h3>${title}</h3><p>${description}</p></div>`;
}

export function errorState(message) {
  return `<div class="error-state"><p class="eyebrow">SEARCH ERROR</p><h2>ログを取得できませんでした</h2><p>${escapeHtml(message)}</p></div>`;
}

export function renderError(message) {
  document.querySelector("#app").innerHTML = `
    <section class="error-state page-error">
      <div class="error-icon" aria-hidden="true">!</div>
      <p class="eyebrow">CONNECTION ISSUE</p>
      <h1>ログを読み込めませんでした</h1>
      <p>${escapeHtml(message)}</p>
      <div class="error-actions">
        <button class="button-primary" type="button" data-retry>もう一度試す</button>
        <a class="button-secondary" href="/health" data-route>稼働状況を確認</a>
      </div>
    </section>`;
}

function field(name, label, control) {
  return `<label class="filter-field ${name}-field"><span>${label}</span>${control}</label>`;
}

function homeLogCard(log, index) {
  return `
    <article class="home-log-card">
      <div class="log-meta">
        <time>${escapeHtml(log.display_time || log.event_time || "時刻不明")}</time>
        ${resultFilterLink("log_type", log.log_type, log.log_type || "unknown", `log-type ${log.log_type || "unknown"}`)}
      </div>
      <h3>${resultFilterLink("program", log.program, log.program || "unknown program", "source-filter program-filter")}</h3>
      <p class="host-label">${resultFilterLink("host", log.host, log.host || "unknown host", "source-filter host-filter")}</p>
      <p class="message-preview">${escapeHtml(log.msg || "メッセージなし")}</p>
      <button class="card-link" type="button" data-log-index="${index}">ログ詳細を見る <span>→</span></button>
    </article>
  `;
}

function resultFilterLink(key, value, label, className, currentParams = "") {
  if (!value) return `<span class="${escapeHtml(className)}">${escapeHtml(label)}</span>`;
  const params = new URLSearchParams(currentParams);
  params.set(key, value);
  params.set("page", "1");
  return `
    <a
      class="${escapeHtml(className)}"
      href="/search?${escapeHtml(params.toString())}"
      data-route
      title="${escapeHtml(label)}で絞り込む"
      aria-label="${escapeHtml(label)}で絞り込む"
    >${escapeHtml(label)}</a>
  `;
}

function detailRow(label, value, wide = false) {
  return `<div${wide ? ` class="wide"` : ""}><dt>${label}</dt><dd>${escapeHtml(value || "—")}</dd></div>`;
}
