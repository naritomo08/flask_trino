import { requestLogs } from "./api.js";
import { homeLogsMarkup, searchForm } from "./components.js";
import { setCurrentLogs } from "./state.js";
import { defaultSearchParams } from "./search.js";

const app = document.querySelector("#app");
let homeTimer;
let homeRequestId = 0;

export async function renderHomePage() {
  stopHomeMonitoring();
  const requestId = ++homeRequestId;
  const params = defaultSearchParams({ size: 6 });
  document.title = "Trino Log Search";
  app.innerHTML = `<div class="loading-state">ログを読み込んでいます…</div>`;

  const payload = await requestLogs(params);
  if (requestId !== homeRequestId || location.pathname !== "/") return;
  const logs = payload.logs || [];
  setCurrentLogs(logs);
  const total = Number(payload.total ?? payload.count ?? logs.length);

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
        ${homeLogsMarkup(logs)}
      </div>
    </section>
  `;
  homeTimer = window.setInterval(updateHomeLogs, 5000);
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
    const payload = await requestLogs(defaultSearchParams({ size: 6 }));
    if (requestId !== homeRequestId || location.pathname !== "/") return;

    const logs = payload.logs || [];
    setCurrentLogs(logs);
    const total = Number(payload.total ?? payload.count ?? logs.length);
    const totalElement = document.querySelector("[data-home-total]");
    const logsElement = document.querySelector("[data-home-logs]");
    if (totalElement) totalElement.textContent = total.toLocaleString("ja-JP");
    if (logsElement) logsElement.innerHTML = homeLogsMarkup(logs);
  } catch {
    // 初期表示済みの内容を残し、次回の自動更新で再試行します。
  }
}
