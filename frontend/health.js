const backends = [
  { id: "flask", label: "Python / Flask" },
  { id: "go", label: "Go" },
  { id: "java", label: "Java" },
  { id: "php", label: "PHP" },
  { id: "ruby", label: "Ruby" }
];

const healthSummary = document.getElementById("health-summary");
const healthBody = document.getElementById("health-body");
const refreshIntervalMs = 10000;
const healthTimeoutMs = 3000;
let hasRenderedHealth = false;
let isLoadingHealth = false;

loadHealth();
window.setInterval(loadHealth, refreshIntervalMs);

async function loadHealth() {
  if (isLoadingHealth) {
    return;
  }

  isLoadingHealth = true;
  if (!hasRenderedHealth) {
    setHealthSummary("確認中");
    healthBody.replaceChildren(...backends.map((backend) => healthCard(backend, { state: "checking" })));
  }

  try {
    const results = await Promise.all(backends.map(checkBackend));
    const okCount = results.filter((result) => result.ok).length;
    setHealthSummary(`${okCount} / ${results.length} backend OK`, new Date().toLocaleString());
    healthBody.replaceChildren(...results.map((result) => healthCard(result.backend, result)));
    hasRenderedHealth = true;
  } finally {
    isLoadingHealth = false;
  }
}

async function checkBackend(backend) {
  const controller = new AbortController();
  const timeoutId = window.setTimeout(() => controller.abort(), healthTimeoutMs);

  try {
    const response = await fetch(`/health/${backend.id}`, {
      cache: "no-store",
      signal: controller.signal,
      headers: {
        "Accept": "application/json"
      }
    });
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || `HTTP ${response.status}`);
    }
    return { backend, ok: payload.ok === true, payload };
  } catch (error) {
    const message = error.name === "AbortError" ? "health check timed out" : error.message;
    return { backend, ok: false, error: message || "connection failed" };
  } finally {
    window.clearTimeout(timeoutId);
  }
}

function setHealthSummary(...items) {
  healthSummary.replaceChildren(...items.map((item) => {
    const span = document.createElement("span");
    span.textContent = item;
    return span;
  }));
}

function healthCard(backend, result) {
  const article = document.createElement("article");
  article.className = `health-card ${result.ok ? "is-ok" : "is-error"}`;

  const header = document.createElement("div");
  header.className = "health-card-header";

  const title = document.createElement("h2");
  title.textContent = backend.label;

  const status = document.createElement("span");
  status.className = "health-status";
  status.textContent = result.state === "checking" ? "Checking" : result.ok ? "OK" : "NG";

  header.append(title, status);

  const details = document.createElement("dl");
  appendDetail(details, "Backend", result.state === "checking" ? "確認中" : result.error ? "接続失敗" : "接続成功");
  appendDetail(details, "Trino", result.state === "checking" ? "確認中" : result.ok ? "接続成功" : "接続失敗");
  appendDetail(details, "URL", result.payload?.trino_url || "-");
  appendDetail(details, "Catalog", result.payload?.catalog || "-");
  appendDetail(details, "Schema", result.payload?.schema || "-");
  if (result.error) {
    appendDetail(details, "Error", result.error);
  }

  article.append(header, details);
  return article;
}

function appendDetail(list, label, value) {
  const term = document.createElement("dt");
  term.textContent = label;
  const description = document.createElement("dd");
  description.textContent = value;
  list.append(term, description);
}
