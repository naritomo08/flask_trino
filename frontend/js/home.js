import { requestLogs, requestLogSummary } from "./api.js";
import { homeLogsMarkup, searchForm } from "./components.js";
import { setCurrentLogs } from "./state.js";
import { defaultSearchParams } from "./search.js";

const app = document.querySelector("#app");
let homeTimer;
let homeRequestId = 0;

export async function renderHomePage() {
  stopHomeMonitoring();
  const requestId = ++homeRequestId;
  const params = recentLogParams();
  document.title = "Trino Log Search";
  app.innerHTML = `<div class="loading-state">ログを読み込んでいます…</div>`;

  const [payload, summary] = await Promise.all([
    requestLogs(params),
    requestLogSummary(params.date)
  ]);
  if (requestId !== homeRequestId || location.pathname !== "/") return;
  const logs = payload.logs || [];
  const total = Number(summary.total || 0);
  setCurrentLogs(logs);

  app.innerHTML = `
    <section class="hero">
      <p class="eyebrow">OPERATIONAL LOG DISCOVERY</p>
      <h1>必要なログへ、<br>すばやくたどり着く。</h1>
      <p class="hero-copy">Trino / Iceberg に保存されたログを、日付・時刻・ホスト・プログラム・メッセージから横断検索できます。ホストとプログラムは <code>/^web\\d+$/</code> のように入力すると正規表現で検索できます。</p>
      <div class="log-total" aria-label="本日のログ総量 ${total.toLocaleString("ja-JP")}件">
        <span class="log-total-label">本日のログ総量</span>
        <strong class="log-total-value"><span data-home-total>${total.toLocaleString("ja-JP")}</span><small>件</small></strong>
      </div>
      ${searchForm(defaultSearchParams({ size: 25 }), "hero-search")}
    </section>
    <section class="section">
      <div class="section-heading">
        <div>
          <p class="eyebrow">RECENT EVENTS</p>
          <h2>直近1時間のログ</h2>
        </div>
        <a class="text-link" href="/search" data-route>すべて表示 →</a>
      </div>
      <div data-home-logs>
        ${homeLogsMarkup(logs)}
      </div>
    </section>
  `;
  homeTimer = window.setInterval(updateHomeLogs, 30000);
}

export function stopHomeMonitoring() {
  window.clearInterval(homeTimer);
  homeTimer = undefined;
  homeRequestId += 1;
}

async function updateHomeLogs() {
  if (location.pathname !== "/") return;
  const requestId = ++homeRequestId;

  try {
    const params = recentLogParams();
    const [payload, summary] = await Promise.all([
      requestLogs(params),
      requestLogSummary(params.date)
    ]);
    if (requestId !== homeRequestId || location.pathname !== "/") return;

    const logs = payload.logs || [];
    const total = Number(summary.total || 0);
    setCurrentLogs(logs);
    const totalElement = document.querySelector("[data-home-total]");
    const logsElement = document.querySelector("[data-home-logs]");
    if (totalElement) totalElement.textContent = total.toLocaleString("ja-JP");
    if (logsElement) logsElement.innerHTML = homeLogsMarkup(logs);
  } catch {
    // 初期表示済みの内容を残し、次回の自動更新で再試行します。
  }
}

function recentLogParams(now = new Date()) {
  const end = formatJstDateTime(now);
  const start = formatJstDateTime(new Date(now.getTime() - 60 * 60 * 1000));
  return defaultSearchParams({
    date: end.slice(0, 10),
    time_from: start,
    time_to: end,
    size: 6,
    skip_total: 1
  });
}

function formatJstDateTime(date) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Tokyo",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23"
  }).formatToParts(date);
  const value = Object.fromEntries(parts.map(({ type, value: part }) => [type, part]));
  return `${value.year}-${value.month}-${value.day}T${value.hour}:${value.minute}:${value.second}+09:00`;
}
