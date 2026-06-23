export function todayJst() {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Tokyo", year: "numeric", month: "2-digit", day: "2-digit"
  }).format(new Date());
}

export function highlight(value, query) {
  const escaped = escapeHtml(value);
  if (!query) return escaped;
  const pattern = new RegExp(escapeRegExp(escapeHtml(query)), "gi");
  return escaped.replace(pattern, (match) => `<mark>${match}</mark>`);
}

export function escapeHtml(value) {
  const element = document.createElement("span");
  element.textContent = String(value ?? "");
  return element.innerHTML;
}

export function positiveInt(value, fallback) {
  const number = Number.parseInt(value, 10);
  return Number.isFinite(number) && number > 0 ? number : fallback;
}

export function downloadCsv(logs) {
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

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function csvCell(value) {
  return `"${String(value ?? "").replaceAll("\"", "\"\"")}"`;
}
