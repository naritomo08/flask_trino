import { requestAccessLogs, requestHealth } from "./api.js";
import { BACKENDS } from "./config.js";
import { downloadAccessLogsCsv, escapeHtml, todayJst } from "./utils.js";

const app = document.querySelector("#app");
let healthTimer;
let healthRequestId = 0;
let accessLogs = [];
let accessLogDate = "";

export async function renderHealthPage() {
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
      <section class="access-log-section">
        <div class="access-log-heading">
          <div>
            <p class="eyebrow">ACCESS LOGS</p>
            <h2>アクセスログ</h2>
            <p>監視リクエストと静的ファイルを除いた、実際のブラウザ操作を表示します。</p>
          </div>
          <form class="access-log-controls" data-access-log-form>
            <label>対象日 <input type="date" name="date" value="${todayJst()}"></label>
            <button class="button-secondary" type="submit">表示</button>
            <button class="button-secondary" type="button" data-access-log-download>CSVダウンロード</button>
          </form>
        </div>
        <div class="access-log-summary" data-access-log-summary>読み込み中…</div>
        <div class="access-log-table-wrap" data-access-log-table></div>
      </section>
    </section>
  `;
  accessLogDate = todayJst();
  await updateHealth();
  healthTimer = window.setInterval(updateHealth, 5000);
}

export async function updateHealth() {
  if (location.pathname !== "/health") return;
  const requestId = ++healthRequestId;
  const results = await Promise.all(Object.keys(BACKENDS).map(requestHealth));
  if (requestId !== healthRequestId || location.pathname !== "/health") return;

  results.forEach(updateHealthCard);
  const healthy = results.filter((result) => result.backendOk && result.trinoOk).length;
  const summary = document.querySelector("[data-health-summary]");
  const dot = document.querySelector(".status-dot");
  const updated = document.querySelector("[data-health-updated]");
  summary.textContent = healthy === results.length ? "すべてのサービスが正常です" : `${healthy}/${results.length} サービスが正常です`;
  dot.className = `status-dot ${healthy === results.length ? "healthy" : "unhealthy"}`;
  updated.textContent = `最終更新 ${new Date().toLocaleTimeString("ja-JP")}`;
  await updateAccessLogs({ date: accessLogDate });
}

export async function updateAccessLogs({ date = accessLogDate } = {}) {
  if (location.pathname !== "/health") return;
  accessLogDate = String(date || todayJst());
  const dateInput = document.querySelector("[data-access-log-form] input[name=date]");
  const summary = document.querySelector("[data-access-log-summary]");
  const table = document.querySelector("[data-access-log-table]");
  if (!summary || !table) return;
  if (dateInput) dateInput.value = accessLogDate;
  try {
    const payload = await requestAccessLogs({ date: accessLogDate, tail: 100 });
    accessLogs = payload.logs || [];
    summary.textContent = `${payload.date} / ${payload.count}件（直近100件まで）`;
    table.innerHTML = accessLogTable(accessLogs);
  } catch (error) {
    accessLogs = [];
    summary.textContent = "取得失敗";
    table.innerHTML = accessLogTable([], error.message);
  }
}

export async function downloadCurrentAccessLogs() {
  try {
    const payload = await requestAccessLogs({ date: accessLogDate, full: true });
    downloadAccessLogsCsv(payload.logs || [], payload.date);
  } catch (error) {
    const summary = document.querySelector("[data-access-log-summary]");
    if (summary) summary.textContent = error.message;
  }
}

export function stopHealthMonitoring() {
  window.clearInterval(healthTimer);
  healthTimer = undefined;
  healthRequestId += 1;
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

function accessLogTable(logs, message = "") {
  return `
    <table class="access-log-table">
      <thead><tr><th>時刻</th><th>接続元</th><th>Method</th><th>URI</th><th>Status</th><th>応答時間</th></tr></thead>
      <tbody>
        ${logs.length ? [...logs].reverse().map((log) => `
          <tr>
            <td>${escapeHtml(formatAccessTime(log["@timestamp"]))}</td>
            <td>${escapeHtml(log.remote_addr || "—")}</td>
            <td>${escapeHtml(log.method || "—")}</td>
            <td class="access-log-uri" title="${escapeHtml(log.uri || "")}">${escapeHtml(log.uri || "—")}</td>
            <td class="${Number(log.status) >= 400 ? "is-error" : ""}">${escapeHtml(log.status ?? "—")}</td>
            <td>${escapeHtml(log.request_time ?? "—")} s</td>
          </tr>`).join("") : `<tr><td colspan="6">${escapeHtml(message || "対象日のアクセスログはありません。")}</td></tr>`}
      </tbody>
    </table>`;
}

function formatAccessTime(value) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? value
    : date.toLocaleString("ja-JP", { timeZone: "Asia/Tokyo" });
}
