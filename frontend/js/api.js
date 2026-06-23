import { getSelectedBackend } from "./state.js";

export async function backendIsAvailable(key) {
  try {
    const response = await fetch(`/health/${key}`, { cache: "no-store" });
    const payload = await readJson(response);
    return response.ok && typeof payload.ok === "boolean";
  } catch {
    return false;
  }
}

export async function requestLogs(params) {
  const response = await fetch(apiPath("logs"), {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(params)
  });
  const payload = await readJson(response);
  if (!response.ok) throw new Error(payload.error || "検索に失敗しました。");
  return payload;
}

export async function requestHealth(key) {
  const started = performance.now();
  try {
    const response = await fetch(`/health/${key}`, { cache: "no-store" });
    const payload = await readJson(response);
    return {
      key,
      backendOk: response.ok,
      trinoOk: payload.ok === true,
      latency: Math.round(performance.now() - started)
    };
  } catch {
    return {
      key,
      backendOk: false,
      trinoOk: false,
      latency: Math.round(performance.now() - started)
    };
  }
}

export async function requestAccessLogs({ date = "", full = false, tail = 200 } = {}) {
  const params = new URLSearchParams();
  if (date) params.set("date", date);
  if (full) params.set("full", "1");
  else params.set("tail", String(tail));
  const response = await fetch(`/api/access-logs?${params}`, { cache: "no-store" });
  const payload = await readJson(response);
  if (!response.ok) {
    const message = response.status >= 500
      ? "アクセスログAPIへ接続できませんでした。サービスの稼働状況を確認してください。"
      : payload.error || "アクセスログを取得できませんでした。";
    throw new Error(message);
  }
  return payload;
}

async function readJson(response) {
  const text = await response.text();
  try {
    return JSON.parse(text);
  } catch {
    return { error: httpErrorMessage(response) };
  }
}

function apiPath(endpoint) {
  return endpoint === "health"
    ? `/health/${getSelectedBackend()}`
    : `/api/${getSelectedBackend()}/${endpoint}`;
}

function httpErrorMessage(response) {
  if (response.status === 504) {
    return "検索処理がタイムアウトしました。条件を絞って、もう一度お試しください。";
  }
  if (response.status === 502) {
    return "バックエンドまたはTrinoへの接続に失敗しました。稼働状況を確認してください。";
  }
  if (response.status === 503) {
    return "サービスを一時的に利用できません。しばらくしてから、もう一度お試しください。";
  }
  return response.statusText || "サーバーから正常な応答を受け取れませんでした。";
}
