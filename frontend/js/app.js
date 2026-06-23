import { backendIsAvailable } from "./api.js";
import { showLogDetail, renderError } from "./components.js";
import { BACKENDS } from "./config.js";
import {
  downloadCurrentAccessLogs,
  renderHealthPage,
  stopHealthMonitoring,
  updateAccessLogs,
  updateHealth
} from "./health.js";
import { renderHomePage, stopHomeMonitoring } from "./home.js";
import { renderSearchPage } from "./search.js";
import { getCurrentLogs, getSelectedBackend, setSelectedBackend } from "./state.js";
import { downloadCsv } from "./utils.js";

const app = document.querySelector("#app");
const backendSelect = document.querySelector("#backend-select");
const logDialog = document.querySelector("#log-dialog");

backendSelect.addEventListener("change", async () => {
  setSelectedBackend(backendSelect.value);
  app.innerHTML = `<div class="loading-state">${BACKENDS[getSelectedBackend()].label}バックエンドへ切り替え中…</div>`;
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

  const detailButton = event.target.closest("[data-log-index]");
  if (detailButton) {
    showLogDetail(getCurrentLogs()[Number(detailButton.dataset.logIndex)]);
    return;
  }

  if (event.target.closest("[data-download-csv]")) {
    downloadCsv(getCurrentLogs());
    return;
  }

  if (event.target.closest("[data-health-refresh]")) {
    await updateHealth();
    return;
  }

  if (event.target.closest("[data-access-log-download]")) {
    await downloadCurrentAccessLogs();
    return;
  }

  if (event.target.closest("[data-dialog-close]")) {
    logDialog.close();
  }
});

document.addEventListener("submit", async (event) => {
  if (event.target.matches("[data-access-log-form]")) {
    event.preventDefault();
    await updateAccessLogs({ date: new FormData(event.target).get("date") });
    return;
  }
  if (!event.target.matches("[data-search-form]")) return;
  event.preventDefault();
  stopMonitoring();
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
    stopMonitoring();
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
    setSelectedBackend(null);
  } else {
    availableBackends.forEach((key) => backendSelect.add(new Option(BACKENDS[key].label, key)));
    const storedBackend = getSelectedBackend();
    const selectedBackend = availableBackends.includes(storedBackend)
      ? storedBackend
      : availableBackends.includes("elixir") ? "elixir" : availableBackends[0];
    setSelectedBackend(selectedBackend);
    backendSelect.value = selectedBackend;
  }

  await renderRoute();
}

async function renderRoute() {
  stopMonitoring();
  window.scrollTo({ top: 0 });
  try {
    if (location.pathname === "/health") {
      await renderHealthPage();
    } else if (!getSelectedBackend()) {
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

function stopMonitoring() {
  stopHealthMonitoring();
  stopHomeMonitoring();
}

function isReloadNavigation() {
  const navigationEntry = performance.getEntriesByType?.("navigation")[0];
  if (navigationEntry) return navigationEntry.type === "reload";
  return performance.navigation?.type === 1;
}
