const searchForm = document.getElementById("search-form");
const resultsSummary = document.getElementById("results-summary");
let resultsBody = document.getElementById("results-body");

if (searchForm && resultsSummary && resultsBody) {
  searchForm.addEventListener("submit", async (event) => {
    event.preventDefault();

    setSummary("検索中");
    replaceResultsBody(emptyMessage("検索中", "empty searching"));

    try {
      const response = await fetch("/api/logs", {
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
      setSummary("検索に失敗しました");
      replaceResultsBody(emptyMessage(error.message || "検索に失敗しました。", "empty error"));
    }
  });

  searchForm.addEventListener("reset", () => {
    window.setTimeout(() => {
      setSummary("検索を実施してください");
      replaceResultsBody(emptyMessage("検索条件を入力して検索ボタンを押してください。"));
    }, 0);
  });
}

function formPayload(form) {
  return Object.fromEntries(new FormData(form).entries());
}

function setSummary(...items) {
  resultsSummary.replaceChildren(...items.map((item) => {
    const span = document.createElement("span");
    span.textContent = item;
    return span;
  }));
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
