import { requestHealth } from "./api.js";
import { BACKENDS } from "./config.js";

const app = document.querySelector("#app");
let healthTimer;
let healthRequestId = 0;

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
    </section>
  `;
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
