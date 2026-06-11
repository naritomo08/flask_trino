const searchForm = document.getElementById("search-form");
const resultsSummary = document.getElementById("results-summary");
const resultsBody = document.getElementById("results-body");

if (searchForm && resultsSummary && resultsBody) {
  searchForm.addEventListener("submit", () => {
    resultsSummary.innerHTML = "<span>検索中</span>";
    resultsBody.className = "empty searching";
    resultsBody.textContent = "検索中";
  });
}
