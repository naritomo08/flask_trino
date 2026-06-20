const BACKENDS = {
  elixir: { label: "Elixir" },
  flask: { label: "Python" },
  go: { label: "Go" },
  java: { label: "Java" },
  php: { label: "PHP" },
  ruby: { label: "Ruby" }
};

const app = document.querySelector("#app");
const backendSelect = document.querySelector("#backend-select");
const logDialog = document.querySelector("#log-dialog");
const logDetail = document.querySelector("#log-detail");
let selectedBackend = localStorage.getItem("trino-log-search-backend");
let healthTimer;
let healthRequestId = 0;
let homeTimer;
let homeRequestId = 0;
let currentLogs = [];

backendSelect.addEventListener("change", async () => {
  selectedBackend = backendSelect.value;
  localStorage.setItem("trino-log-search-backend", selectedBackend);
  app.innerHTML = `<div class="loading-state">${BACKENDS[selectedBackend].label}バックエンドへ切り替え中…</div>`;
  await renderRoute();
});

window.addEventListener("popstate", renderRoute);

document.addEventListener("click", async (event) => {
  const route = event.target.closest("a[data-route]");
  if (route && route.origin === location.origin) {
    event.preventDefault();
    history.pushState({}, "", route.href);
    await renderRoute();
    return;
  }

  const pageLink = event.target.closest("[data-page]");
  if (pageLink) {
    const params = new URLSearchParams(location.search);
    params.set("page", pageLink.dataset.page);
    history.pushState({}, "", `/search?${params}`);
    await renderSearchPage();
    return;
  }

  const filterButton = event.target.closest("[data-result-filter]");
  if (filterButton) {
    const params = new URLSearchParams(location.search);
    params.set(filterButton.dataset.resultFilter, filterButton.dataset.filterValue);
    params.set("page", "1");
    history.pushState({}, "", `/search?${params}`);
    await renderSearchPage();
    return;
  }

  const detailButton = event.target.closest("[data-log-index]");
  if (detailButton) {
    showLogDetail(currentLogs[Number(detailButton.dataset.logIndex)]);
    return;
  }

  if (event.target.closest("[data-download-csv]")) {
    downloadCsv(currentLogs);
    return;
  }

  if (event.target.closest("[data-health-refresh]")) {
    await updateHealth();
    return;
  }

  if (event.target.closest("[data-dialog-close]")) {
    logDialog.close();
  }
});

document.addEventListener("submit", async (event) => {
  if (!event.target.matches("[data-search-form]")) return;
  event.preventDefault();
  const params = new URLSearchParams();
  const values = new FormData(event.target);
  for (const [key, value] of values.entries()) {
    if (String(value).trim()) params.set(key, value);
  }
  params.set("page", "1");
  history.pushState({}, "", `/search?${params}`);
  await renderSearchPage();
});

document.addEventListener("reset", (event) => {
  if (!event.target.matches("[data-search-form]")) return;
  window.setTimeout(() => {
    history.pushState({}, "", "/search");
    renderSearchPage();
  }, 0);
});

logDialog.addEventListener("click", (event) => {
  if (event.target === logDialog) logDialog.close();
});

if (isReloadNavigation() && location.pathname !== "/") {
  location.replace("/");
} else {
  initializeApp();
}

function isReloadNavigation() {
  const navigationEntry = performance.getEntriesByType?.("navigation")[0];
  if (navigationEntry) return navigationEntry.type === "reload";

  return performance.navigation?.type === 1;
}

async function initializeApp() {
  const availability = await Promise.all(
    Object.keys(BACKENDS).map(async (key) => [key, await backendIsAvailable(key)])
  );
  const availableBackends = availability
    .filter(([, available]) => available)
    .map(([key]) => key);

  backendSelect.replaceChildren();
  if (!availableBackends.length) {
    const option = new Option("利用可能なBackendなし", "");
    backendSelect.add(option);
    backendSelect.disabled = true;
    selectedBackend = null;
  } else {
    availableBackends.forEach((key) => backendSelect.add(new Option(BACKENDS[key].label, key)));
    selectedBackend = availableBackends.includes(selectedBackend)
      ? selectedBackend
      : availableBackends.includes("elixir") ? "elixir" : availableBackends[0];
    backendSelect.value = selectedBackend;
    localStorage.setItem("trino-log-search-backend", selectedBackend);
  }

  await renderRoute();
}

async function backendIsAvailable(key) {
  try {
    const response = await fetch(`/health/${key}`, { cache: "no-store" });
    const payload = await readJson(response);
    return response.ok && typeof payload.ok === "boolean";
  } catch {
    return false;
  }
}

async function renderRoute() {
  stopHealthMonitoring();
  window.scrollTo({ top: 0 });
  try {
    if (location.pathname === "/health") {
      await renderHealthPage();
    } else if (!selectedBackend) {
      renderError("利用可能なバックエンドがありません。稼働状況を確認してください。");
    } else if (location.pathname === "/search") {
      await renderSearchPage();
    } else {
      await renderHomePage();
    }
  } catch (error) {
    renderError(error.message || "画面を表示できませんでした。");
  }
}

async function renderHomePage() {
  stopHealthMonitoring();
  const requestId = ++homeRequestId;
  const params = defaultSearchParams({ size: 6 });
  document.title = "Trino Log Search";
  app.innerHTML = `<div class="loading-state">ログを読み込んでいます…</div>`;

  const payload = await requestLogs(params);
  if (requestId !== homeRequestId || location.pathname !== "/") return;
  currentLogs = payload.logs || [];
  const total = Number(payload.total ?? payload.count ?? currentLogs.length);

  app.innerHTML = `
    <section class="hero">
      <p class="eyebrow">OPERATIONAL LOG DISCOVERY</p>
      <h1>必要なログへ、<br>すばやくたどり着く。</h1>
      <p class="hero-copy">Trino / Iceberg に保存されたログを、日付・時刻・ホスト・プログラム・メッセージから横断検索できます。ホストとプログラムは <code>/^web\\d+$/</code> のように入力すると正規表現で検索できます。</p>
      <div class="log-total" aria-label="現在のログ総量 ${total.toLocaleString("ja-JP")}件">
        <span class="log-total-label">現在のログ総量</span>
        <strong class="log-total-value"><span data-home-total>${total.toLocaleString("ja-JP")}</span><small>件</small></strong>
      </div>
      ${searchForm({ ...params, size: 25 }, "hero-search")}
    </section>
    <section class="section">
      <div class="section-heading">
        <div>
          <p class="eyebrow">RECENT EVENTS</p>
          <h2>最近のログ</h2>
        </div>
        <a class="text-link" href="/search" data-route>すべて表示 →</a>
      </div>
      <div data-home-logs>
        ${homeLogsMarkup(currentLogs)}
      </div>
    </section>
  `;
  homeTimer = window.setInterval(updateHomeLogs, 5000);
}

async function updateHomeLogs() {
  if (location.pathname !== "/") return;
  const requestId = ++homeRequestId;

  try {
    const payload = await requestLogs(defaultSearchParams({ size: 6 }));
    if (requestId !== homeRequestId || location.pathname !== "/") return;

    currentLogs = payload.logs || [];
    const total = Number(payload.total ?? payload.count ?? currentLogs.length);
    const totalElement = document.querySelector("[data-home-total]");
    const logsElement = document.querySelector("[data-home-logs]");
    if (totalElement) totalElement.textContent = total.toLocaleString("ja-JP");
    if (logsElement) logsElement.innerHTML = homeLogsMarkup(currentLogs);
  } catch {
    // 初期表示済みの内容を残し、次回の自動更新で再試行します。
  }
}

function homeLogsMarkup(logs) {
  return logs.length
    ? `<div class="log-grid">${logs.map(homeLogCard).join("")}</div>`
    : emptyState("ログがありません", "対象日のログがまだ登録されていません。");
}

async function renderSearchPage() {
  stopHealthMonitoring();
  const params = searchParams();
  document.title = "ログ検索 | Trino Log Search";
  app.innerHTML = `<div class="loading-state">ログを検索しています…</div>`;

  try {
    const payload = await requestLogs(params);
    currentLogs = payload.logs || [];
    renderSearchResults(payload, params);
  } catch (error) {
    currentLogs = [];
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

function searchForm(params, className = "") {
  return `
    <form class="search-form ${className}" data-search-form>
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

function field(name, label, control) {
  return `<label class="filter-field ${name}-field"><span>${label}</span>${control}</label>`;
}

async function requestLogs(params) {
  const response = await fetch(apiPath("logs"), {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(params)
  });
  const payload = await readJson(response);
  if (!response.ok) throw new Error(payload.error || "検索に失敗しました。");
  return payload;
}

function renderSearchResults(payload, params) {
  const total = Number(payload.total ?? payload.count ?? currentLogs.length);
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
        ${currentLogs.length ? `<button class="button-secondary compact" type="button" data-download-csv>CSVダウンロード</button>` : ""}
      </div>
      ${currentLogs.length
        ? `<div class="result-list">${currentLogs.map((log, index) => resultCard(log, index, params.message)).join("")}</div>${pagination(page, totalPages)}`
        : emptyState("一致するログがありません", "条件を減らすか、検索期間を広げてみてください。")}
    </section>
  `;
}

function homeLogCard(log, index) {
  return `
    <article class="home-log-card">
      <div class="log-meta">
        <time>${escapeHtml(log.display_time || log.event_time || "時刻不明")}</time>
        ${resultFilterButton("log_type", log.log_type, log.log_type || "unknown", `log-type ${log.log_type || "unknown"}`)}
      </div>
      <h3>${resultFilterButton("program", log.program, log.program || "unknown program", "source-filter program-filter")}</h3>
      <p class="host-label">${resultFilterButton("host", log.host, log.host || "unknown host", "source-filter host-filter")}</p>
      <p class="message-preview">${escapeHtml(log.msg || "メッセージなし")}</p>
      <button class="card-link" type="button" data-log-index="${index}">ログ詳細を見る <span>→</span></button>
    </article>
  `;
}

function resultCard(log, index, keyword) {
  return `
    <article class="result-card">
      <div class="result-card-top">
        <div class="log-meta">
          <time>${escapeHtml(log.display_time || log.event_time || "—")}</time>
          ${resultFilterButton("log_type", log.log_type, log.log_type || "unknown", `log-type ${log.log_type || "unknown"}`)}
        </div>
        <span class="index-name">${escapeHtml(log.index || "")}</span>
      </div>
      <div class="result-identity">
        ${resultFilterButton("host", log.host, log.host || "unknown host", "source-filter host-filter")}
        <span>/</span>
        ${resultFilterButton("program", log.program, log.program || "unknown program", "source-filter")}
      </div>
      <p class="result-message">${highlight(log.msg || "", keyword)}</p>
      <button class="card-link" type="button" data-log-index="${index}">すべてのフィールドを表示 <span>→</span></button>
    </article>
  `;
}

function resultFilterButton(key, value, label, className) {
  if (!value) return `<span class="${escapeHtml(className)}">${escapeHtml(label)}</span>`;
  return `
    <button
      class="${escapeHtml(className)}"
      type="button"
      data-result-filter="${escapeHtml(key)}"
      data-filter-value="${escapeHtml(value)}"
      title="${escapeHtml(label)}で絞り込む"
      aria-label="${escapeHtml(label)}で絞り込む"
    >${escapeHtml(label)}</button>
  `;
}

function pagination(page, totalPages) {
  if (totalPages <= 1) return "";
  return `
    <nav class="pagination" aria-label="検索結果ページ">
      ${page > 1 ? `<button type="button" data-page="${page - 1}">← 前へ</button>` : `<span class="disabled">← 前へ</span>`}
      <span>${page} / ${totalPages}</span>
      ${page < totalPages ? `<button type="button" data-page="${page + 1}">次へ →</button>` : `<span class="disabled">次へ →</span>`}
    </nav>
  `;
}

function showLogDetail(log) {
  if (!log) return;
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

function detailRow(label, value, wide = false) {
  return `<div${wide ? ` class="wide"` : ""}><dt>${label}</dt><dd>${escapeHtml(value || "—")}</dd></div>`;
}

async function renderHealthPage() {
  document.title = "稼働状況 | Trino Log Search";
  app.innerHTML = `
    <section class="health-page">
      <div class="page-heading">
        <div>
          <p class="eyebrow">SYSTEM HEALTH</p>
          <h1>稼働状況</h1>
          <p>各バックエンドと、それぞれからTrinoへの接続状態を確認します。</p>
        </div>
        <button class="button-secondary" type="button" data-health-refresh>↻ 今すぐ更新</button>
      </div>
      <div class="health-summary">
        <span class="status-dot checking"></span>
        <strong data-health-summary>確認中…</strong>
        <time data-health-updated></time>
      </div>
      <div class="health-grid">
        ${Object.entries(BACKENDS).map(([key, backend]) => healthCard(key, backend)).join("")}
      </div>
    </section>
  `;
  await updateHealth();
  healthTimer = window.setInterval(updateHealth, 5000);
}

function healthCard(key, backend) {
  return `
    <article class="health-card checking" data-health-card="${key}">
      <div class="health-card-top">
        <span class="service-icon">${backend.label.slice(0, 1)}</span>
        <span class="health-badge">確認中</span>
      </div>
      <h2>${backend.label}</h2>
      <p class="health-message">応答を確認しています。</p>
      <dl>
        <div><dt>Backend</dt><dd data-backend-state>—</dd></div>
        <div><dt>Trino</dt><dd data-trino-state>—</dd></div>
        <div><dt>応答時間</dt><dd data-latency>—</dd></div>
      </dl>
    </article>
  `;
}

async function updateHealth() {
  if (location.pathname !== "/health") return;
  const requestId = ++healthRequestId;
  const results = await Promise.all(Object.keys(BACKENDS).map(async (key) => {
    const started = performance.now();
    try {
      const response = await fetch(`/health/${key}`, { cache: "no-store" });
      const payload = await readJson(response);
      return { key, backendOk: response.ok, trinoOk: payload.ok === true, latency: Math.round(performance.now() - started) };
    } catch {
      return { key, backendOk: false, trinoOk: false, latency: Math.round(performance.now() - started) };
    }
  }));
  if (requestId !== healthRequestId || location.pathname !== "/health") return;

  results.forEach(updateHealthCard);
  const healthy = results.filter((result) => result.backendOk && result.trinoOk).length;
  const summary = document.querySelector("[data-health-summary]");
  const dot = document.querySelector(".status-dot");
  const updated = document.querySelector("[data-health-updated]");
  summary.textContent = healthy === results.length ? "すべてのサービスが正常です" : `${healthy}/${results.length} サービスが正常です`;
  dot.className = `status-dot ${healthy === results.length ? "healthy" : "unhealthy"}`;
  updated.textContent = `最終更新 ${new Date().toLocaleTimeString("ja-JP")}`;
}

function updateHealthCard(result) {
  const card = document.querySelector(`[data-health-card="${result.key}"]`);
  const healthy = result.backendOk && result.trinoOk;
  card.className = `health-card ${healthy ? "healthy" : "unhealthy"}`;
  card.querySelector(".health-badge").textContent = healthy ? "稼働中" : result.backendOk ? "Trino接続不可" : "停止";
  card.querySelector(".health-message").textContent = healthy
    ? "バックエンドとTrinoが正常に応答しています。"
    : result.backendOk ? "バックエンドは応答していますが、Trinoへ接続できません。" : "バックエンドから応答がありません。";
  card.querySelector("[data-backend-state]").textContent = result.backendOk ? "正常" : "応答なし";
  card.querySelector("[data-trino-state]").textContent = result.trinoOk ? "正常" : "接続不可";
  card.querySelector("[data-latency]").textContent = `${result.latency} ms`;
}

function stopHealthMonitoring() {
  window.clearInterval(healthTimer);
  healthTimer = undefined;
  healthRequestId += 1;
  window.clearInterval(homeTimer);
  homeTimer = undefined;
  homeRequestId += 1;
}

function searchParams() {
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

function defaultSearchParams(overrides = {}) {
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

function todayJst() {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Tokyo", year: "numeric", month: "2-digit", day: "2-digit"
  }).format(new Date());
}

function apiPath(endpoint) {
  return endpoint === "health" ? `/health/${selectedBackend}` : `/api/${selectedBackend}/${endpoint}`;
}

async function readJson(response) {
  const text = await response.text();
  try {
    return JSON.parse(text);
  } catch {
    return { error: text.trim() || response.statusText };
  }
}

function highlight(value, query) {
  const escaped = escapeHtml(value);
  if (!query) return escaped;
  const pattern = new RegExp(escapeRegExp(escapeHtml(query)), "gi");
  return escaped.replace(pattern, (match) => `<mark>${match}</mark>`);
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function emptyState(title, description) {
  return `<div class="empty-state"><h3>${title}</h3><p>${description}</p></div>`;
}

function errorState(message) {
  return `<div class="error-state"><p class="eyebrow">SEARCH ERROR</p><h2>ログを取得できませんでした</h2><p>${escapeHtml(message)}</p></div>`;
}

function renderError(message) {
  app.innerHTML = `<section class="error-state page-error"><p class="eyebrow">APPLICATION ERROR</p><h1>画面を表示できませんでした</h1><p>${escapeHtml(message)}</p><a class="button-secondary" href="/" data-route>トップページへ戻る</a></section>`;
}

function downloadCsv(logs) {
  if (!logs.length) return;
  const keys = [["display_time", "Time"], ["log_type", "Log"], ["host", "Host"], ["program", "Program"], ["msg", "Message"]];
  const rows = [keys.map(([, label]) => label), ...logs.map((log) => keys.map(([key]) => log[key] || ""))];
  const csv = rows.map((row) => row.map(csvCell).join(",")).join("\r\n");
  const url = URL.createObjectURL(new Blob([`\uFEFF${csv}\r\n`], { type: "text/csv;charset=utf-8" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = `logs-${Date.now()}.csv`;
  link.click();
  URL.revokeObjectURL(url);
}

function csvCell(value) {
  return `"${String(value ?? "").replaceAll("\"", "\"\"")}"`;
}

function positiveInt(value, fallback) {
  const number = Number.parseInt(value, 10);
  return Number.isFinite(number) && number > 0 ? number : fallback;
}

function escapeHtml(value) {
  const element = document.createElement("span");
  element.textContent = String(value ?? "");
  return element.innerHTML;
}
