const BACKENDS = {
  elixir: { label: "Elixir" },
  flask: { label: "Python" },
  go: { label: "Go" },
  java: { label: "Java" },
  php: { label: "PHP" },
  ruby: { label: "Ruby" }
};

const app = document.querySelector("#app");
const apiLink = document.querySelector("#api-link");
const backendSelect = document.querySelector("#backend-select");
const logDialog = document.querySelector("#log-dialog");
const logDetail = document.querySelector("#log-detail");
let selectedBackend = localStorage.getItem("trino-log-search-backend");
let healthTimer;
let healthRequestId = 0;
let currentLogs = [];

if (!BACKENDS[selectedBackend]) selectedBackend = "elixir";
backendSelect.value = selectedBackend;

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
    history.pushState({}, "", `/?${params}`);
    await renderSearchPage();
    return;
  }

  const filterButton = event.target.closest("[data-result-filter]");
  if (filterButton) {
    const params = new URLSearchParams(location.search);
    params.set(filterButton.dataset.resultFilter, filterButton.dataset.filterValue);
    params.set("page", "1");
    history.pushState({}, "", `/?${params}`);
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
  history.pushState({}, "", `/?${params}`);
  await renderSearchPage();
});

document.addEventListener("reset", (event) => {
  if (!event.target.matches("[data-search-form]")) return;
  window.setTimeout(() => {
    history.pushState({}, "", "/");
    renderSearchPage();
  }, 0);
});

logDialog.addEventListener("click", (event) => {
  if (event.target === logDialog) logDialog.close();
});

renderRoute();

async function renderRoute() {
  stopHealthMonitoring();
  window.scrollTo({ top: 0 });
  try {
    if (location.pathname === "/health") {
      await renderHealthPage();
    } else {
      await renderSearchPage();
    }
  } catch (error) {
    renderError(error.message || "画面を表示できませんでした。");
  }
}

async function renderSearchPage() {
  stopHealthMonitoring();
  const params = searchParams();
  document.title = "Trino Log Search";
  apiLink.href = apiUrl("logs", params);

  app.innerHTML = `
    <section class="hero">
      <p class="eyebrow">LOG DISCOVERY</p>
      <h1>必要なログへ、<br>すばやくたどり着く。</h1>
      <p class="hero-copy">Trino / Iceberg に保存されたログを、日付・時刻・ホスト・プログラム・メッセージから横断検索できます。</p>
    </section>
    ${searchForm(params)}
    <section id="results-section" class="section">
      <div class="loading-panel">ログを検索しています…</div>
    </section>
  `;

  try {
    const payload = await requestLogs(params);
    currentLogs = payload.logs || [];
    renderResults(payload, params);
  } catch (error) {
    currentLogs = [];
    document.querySelector("#results-section").innerHTML = errorState(error.message);
  }
}

function searchForm(params) {
  return `
    <form class="filter-panel" data-search-form>
      <div class="filter-heading">
        <div>
          <p class="eyebrow">SEARCH FILTERS</p>
          <h2>検索条件</h2>
        </div>
        <span>時刻はJSTです</span>
      </div>
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
        ${field("host", "ホスト", `<input type="search" name="host" value="${escapeHtml(params.host)}" placeholder="例: elastic1">`)}
        ${field("program", "プログラム", `<input type="search" name="program" value="${escapeHtml(params.program)}" placeholder="例: sshd">`)}
        <label class="filter-field message-field">
          <span>メッセージ</span>
          <input type="search" name="message" value="${escapeHtml(params.message)}" placeholder="例: accepted, timeout">
        </label>
        ${field("size", "表示件数", `
          <select name="size">
            ${[10, 25, 50, 100].map((size) => `<option value="${size}"${Number(params.size) === size ? " selected" : ""}>${size}件</option>`).join("")}
          </select>`)}
      </div>
      <div class="filter-actions">
        <button class="button-secondary" type="reset">条件をクリア</button>
        <button class="button-primary" type="submit">ログを検索</button>
      </div>
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

function renderResults(payload, params) {
  const section = document.querySelector("#results-section");
  const total = Number(payload.total ?? payload.count ?? currentLogs.length);
  const page = Number(payload.page || params.page || 1);
  const size = Number(payload.size || params.size || 25);
  const totalPages = Math.max(1, Number(payload.total_pages || Math.ceil(total / size)));

  section.innerHTML = `
    <div class="results-heading">
      <div>
        <p class="eyebrow">SEARCH RESULTS</p>
        <h2>検索結果</h2>
        <p>${activeFilterSummary(params)}</p>
      </div>
      <div class="result-count"><strong>${total.toLocaleString()}</strong><span>件</span></div>
    </div>
    ${currentLogs.length ? `
      <div class="result-toolbar">
        <span>${page} / ${totalPages} ページ</span>
        <button class="button-secondary compact" type="button" data-download-csv>表示中のログをCSV保存</button>
      </div>
      <div class="log-list">${currentLogs.map(logCard).join("")}</div>
      ${pagination(page, totalPages)}
    ` : emptyState("一致するログがありませんでした", "条件を減らすか、時間範囲を広げて検索してください。")}
  `;
}

function logCard(log, index) {
  return `
    <article class="log-card">
      <div class="log-card-top">
        <div class="log-meta">
          <time>${escapeHtml(log.display_time || log.event_time || "—")}</time>
          ${resultFilterButton("log_type", log.log_type, log.log_type || "unknown", `log-type ${log.log_type || "unknown"}`)}
        </div>
        <button class="detail-button" type="button" data-log-index="${index}">詳細</button>
      </div>
      <div class="log-source">
        ${resultFilterButton("host", log.host, log.host || "unknown host", "source-filter host-filter")}
        <span>/</span>
        ${resultFilterButton("program", log.program, log.program || "unknown program", "source-filter")}
      </div>
      <p class="log-message">${highlight(log.msg || "", searchParams().message)}</p>
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
  apiLink.href = apiPath("health");
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
}

function searchParams() {
  const params = new URLSearchParams(location.search);
  return {
    date: params.get("date") || todayJst(),
    time_from: params.get("time_from") || "",
    time_to: params.get("time_to") || "",
    log_type: params.get("log_type") || "",
    host: params.get("host") || "",
    program: params.get("program") || "",
    message: params.get("message") || "",
    page: positiveInt(params.get("page"), 1),
    size: Math.min(positiveInt(params.get("size"), 25), 100)
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

function apiUrl(endpoint, params) {
  const query = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== "") query.set(key, value);
  });
  return `${apiPath(endpoint)}?${query}`;
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
  app.innerHTML = `<section class="error-state page-error"><p class="eyebrow">APPLICATION ERROR</p><h1>画面を表示できませんでした</h1><p>${escapeHtml(message)}</p><a class="button-secondary" href="/" data-route>検索画面へ戻る</a></section>`;
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
