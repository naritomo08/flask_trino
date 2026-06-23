import { requestLogs } from "./api.js";
import { emptyState, errorState, pagination, resultCard, searchForm } from "./components.js";
import { getCurrentLogs, setCurrentLogs } from "./state.js";
import { escapeHtml, positiveInt, todayJst } from "./utils.js";

const app = document.querySelector("#app");

export async function renderSearchPage() {
  const params = searchParams();
  document.title = "ログ検索 | Trino Log Search";
  app.innerHTML = `<div class="loading-state">ログを検索しています…</div>`;

  try {
    const payload = await requestLogs(params);
    setCurrentLogs(payload.logs || []);
    renderSearchResults(payload, params);
  } catch (error) {
    setCurrentLogs([]);
    app.innerHTML = `
      <section class="search-page-header">
        <p class="eyebrow">LOG SEARCH</p>
        <div class="results-summary">
          <div><h1>ログ検索</h1><p>条件はURLに保存されるため、そのまま共有できます。</p></div>
        </div>
        ${searchForm(params, "advanced-search")}
      </section>
      <section class="section">${errorState(error.message)}</section>
    `;
  }
}

export function searchParams() {
  const params = new URLSearchParams(location.search);
  return defaultSearchParams({
    date: params.get("date") || todayJst(),
    time_from: params.get("time_from") || "",
    time_to: params.get("time_to") || "",
    log_type: params.get("log_type") || "",
    host: params.get("host") || "",
    program: params.get("program") || "",
    message: params.get("message") || "",
    page: positiveInt(params.get("page"), 1),
    size: Math.min(positiveInt(params.get("size"), 25), 100)
  });
}

export function defaultSearchParams(overrides = {}) {
  return {
    date: todayJst(),
    time_from: "",
    time_to: "",
    log_type: "",
    host: "",
    program: "",
    message: "",
    page: 1,
    size: 25,
    ...overrides
  };
}

function renderSearchResults(payload, params) {
  const logs = getCurrentLogs();
  const total = Number(payload.total ?? payload.count ?? logs.length);
  const page = Number(payload.page || params.page || 1);
  const size = Number(payload.size || params.size || 25);
  const totalPages = Math.max(1, Number(payload.total_pages || Math.ceil(total / size)));

  app.innerHTML = `
    <section class="search-page-header">
      <p class="eyebrow">LOG SEARCH</p>
      <div class="results-summary">
        <div>
          <h1>ログ検索</h1>
          <p>条件はURLに保存されるため、そのまま共有できます。</p>
        </div>
        <strong>${total.toLocaleString("ja-JP")}<small> 件</small></strong>
      </div>
      ${searchForm(params, "advanced-search")}
    </section>
    <section class="section search-results">
      <div class="result-toolbar">
        <span>${page} / ${totalPages} ページ${activeFilterSummary(params) ? ` · ${activeFilterSummary(params)}` : ""}</span>
        ${logs.length ? `<button class="button-secondary compact" type="button" data-download-csv>CSVダウンロード</button>` : ""}
      </div>
      ${logs.length
        ? `<div class="result-list">${logs.map((log, index) => resultCard(log, index, params.message, location.search)).join("")}</div>${pagination(page, totalPages, location.search)}`
        : emptyState("一致するログがありません", "条件を減らすか、検索期間を広げてみてください。")}
    </section>
  `;
}

function activeFilterSummary(params) {
  const values = [
    params.date,
    params.time_from || params.time_to ? `${params.time_from || "00:00"}–${params.time_to || "23:59"}` : "",
    params.log_type,
    params.host,
    params.program,
    params.message ? `“${params.message}”` : ""
  ].filter(Boolean);
  return escapeHtml(values.join(" / "));
}
