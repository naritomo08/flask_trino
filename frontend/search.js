const searchForm = document.getElementById("search-form");
const resultsSummary = document.getElementById("results-summary");
const resultsSummaryText = document.getElementById("results-summary-text");
const downloadCsvButton = document.getElementById("download-csv");
const backendLanguageSelect = document.getElementById("backend-language");
let resultsBody = document.getElementById("results-body");
let currentLogs = [];

if (searchForm && resultsSummary && resultsBody) {
  searchForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    setCurrentLogs([]);
    setSummary("検索中");
    replaceResultsBody(emptyMessage("検索中", "empty searching"));

    try {
      const response = await fetch(apiPath("logs"), {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Accept": "application/json"
        },
        body: JSON.stringify(formPayload(searchForm))
      });

      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error || "検索に失敗しました。");
      }

      renderResults(payload.logs || []);
    } catch (error) {
      setCurrentLogs([]);
      setSummary("検索に失敗しました");
      replaceResultsBody(emptyMessage(error.message || "検索に失敗しました。", "empty error"));
    }
  });

  searchForm.addEventListener("reset", () => {
    window.setTimeout(() => {
      setCurrentLogs([]);
      setSummary("検索を実施してください");
      replaceResultsBody(emptyMessage("検索条件を入力して検索ボタンを押してください。"));
    }, 0);
  });

  backendLanguageSelect?.addEventListener("change", () => {
    setCurrentLogs([]);
    setSummary(`${selectedBackendLabel()} backend を選択中`);
    replaceResultsBody(emptyMessage("検索条件を入力して検索ボタンを押してください。"));
  });

  downloadCsvButton?.addEventListener("click", () => {
    downloadCsv(currentLogs);
  });
}

function apiPath(endpoint) {
  const backend = backendLanguageSelect?.value || "flask";
  return `/api/${backend}/${endpoint}`;
}

function selectedBackendLabel() {
  return backendLanguageSelect?.selectedOptions[0]?.textContent || "Flask";
}

function formPayload(form) {
  const payload = Object.fromEntries(new FormData(form).entries());
  delete payload.backend_language;
  return payload;
}

function setSummary(...items) {
  const target = resultsSummaryText || resultsSummary;
  target.replaceChildren(...items.map((item) => {
    const span = document.createElement("span");
    span.textContent = item;
    return span;
  }));
}

function setCurrentLogs(logs) {
  currentLogs = logs;
  if (downloadCsvButton) {
    downloadCsvButton.disabled = logs.length === 0;
  }
}

function emptyMessage(message, className = "empty") {
  const element = document.createElement("p");
  element.id = "results-body";
  element.className = className;
  element.textContent = message;
  return element;
}

function replaceResultsBody(element) {
  resultsBody.replaceWith(element);
  resultsBody = element;
}

function renderResults(logs) {
  setCurrentLogs(logs);
  setSummary(`${logs.length} 件`, "最新50件のみ表示");

  if (logs.length === 0) {
    replaceResultsBody(emptyMessage("該当するログはありません。"));
    return;
  }

  const wrapper = document.createElement("div");
  wrapper.id = "results-body";
  wrapper.className = "table-wrap";

  const table = document.createElement("table");
  const thead = document.createElement("thead");
  const headerRow = document.createElement("tr");
  ["Time", "Log", "Host", "Program", "Message"].forEach((label) => {
    const th = document.createElement("th");
    th.textContent = label;
    headerRow.append(th);
  });
  thead.append(headerRow);

  const tbody = document.createElement("tbody");
  logs.forEach((log) => {
    const row = document.createElement("tr");
    appendCell(row, log.display_time || "");
    appendLogTypeCell(row, log.log_type || "unknown");
    appendCell(row, log.host || "");
    appendCell(row, log.program || "");
    appendCell(row, log.msg || "");
    tbody.append(row);
  });

  table.append(thead, tbody);
  wrapper.append(table);
  replaceResultsBody(wrapper);
}

function appendCell(row, value) {
  const cell = document.createElement("td");
  cell.textContent = value;
  row.append(cell);
}

function appendLogTypeCell(row, value) {
  const cell = document.createElement("td");
  const badge = document.createElement("span");
  badge.className = `log-type log-type-${value}`;
  badge.textContent = value;
  cell.append(badge);
  row.append(cell);
}

function downloadCsv(logs) {
  if (logs.length === 0) {
    return;
  }

  const rows = [
    ["Time", "Log", "Host", "Program", "Message"],
    ...logs.map((log) => [
      log.display_time || "",
      log.log_type || "",
      log.host || "",
      log.program || "",
      log.msg || ""
    ])
  ];
  const csv = rows.map((row) => row.map(csvCell).join(",")).join("\r\n");
  const blob = new Blob([`\ufeff${csv}\r\n`], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `log-search-results-${timestampForFilename()}.csv`;
  document.body.append(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 0);
}

function csvCell(value) {
  return `"${String(value).replaceAll('"', '""')}"`;
}

function timestampForFilename() {
  const now = new Date();
  const parts = [
    now.getFullYear(),
    now.getMonth() + 1,
    now.getDate(),
    now.getHours(),
    now.getMinutes(),
    now.getSeconds()
  ];
  return parts.map((part) => String(part).padStart(2, "0")).join("");
}
