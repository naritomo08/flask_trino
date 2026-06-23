let selectedBackend = localStorage.getItem("trino-log-search-backend");
let currentLogs = [];

export function getSelectedBackend() {
  return selectedBackend;
}

export function setSelectedBackend(backend) {
  selectedBackend = backend;
  if (backend) {
    localStorage.setItem("trino-log-search-backend", backend);
  }
}

export function getCurrentLogs() {
  return currentLogs;
}

export function setCurrentLogs(logs) {
  currentLogs = logs;
}
