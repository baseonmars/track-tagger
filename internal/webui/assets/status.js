(() => {
  function progressText(status, refreshReady) {
    if (status.phase === "error") return "Could not validate the library. " + (status.error || "Refresh to retry.");
    if (refreshReady) return "This recording is ready. Refresh it to review the latest details.";
    if (status.validated) return "";
    return `Checking recordings: ${status.scanned} of ${status.total}. Review actions become available after validation.`;
  }
  if (typeof module !== "undefined") module.exports = { progressText };
  if (typeof document === "undefined") return;
  const main = document.querySelector("main[data-phase]");
  if (!main) return;
  const progress = document.getElementById("scan-progress");
  const library = document.getElementById("recording-library");
  const recordingId = main.dataset.recordingId;
  if (!library && !recordingId) return;
  const initialGeneration = Number(main.dataset.generation || 0);
  let generation = initialGeneration;

  function element(tag, className, value) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (value !== undefined) node.textContent = value;
    return node;
  }
  function card(item) {
    const article = element("article", "recording-card");
    const body = element("div", "recording-main");
    body.append(element("span", "status", item.status));
    const heading = element("h2");
    const link = element("a", "", item.title);
    link.href = "/recording?" + new URLSearchParams({id: item.id});
    heading.append(link);
    body.append(heading);
    body.append(element("p", "count", `${item.tracks} audio files${item.date ? " · " + item.date : ""}`));
    const issues = element("ul", "issue-summary");
    for (const issue of item.issues || []) issues.append(element("li", "", issue));
    body.append(issues);
    article.append(body);
    const action = element("a", "button", item.action + " →");
    action.href = link.href;
    article.append(action);
    return article;
  }
  function update(status) {
    const selected = recordingId && (status.recordings || []).find(item => item.id === recordingId);
    const refreshReady = !!(selected && selected.ready && (main.dataset.recordingReady !== "true" || status.generation !== initialGeneration));
    progress.textContent = progressText(status, refreshReady);
    progress.hidden = status.validated && !refreshReady;
    if (library && status.generation !== generation) {
      generation = status.generation;
      const cards = (status.recordings || []).map(card);
      if (cards.length) library.replaceChildren(...cards);
      else {
        const empty = element("section");
        empty.append(element("h2", "", "No recordings found yet"));
        empty.append(element("p", "", "Keep this server running while local files arrive, then refresh this page."));
        library.replaceChildren(empty);
      }
    }
    window.setTimeout(poll, status.validated || status.phase === "error" ? 5000 : 1000);
  }
  async function poll() {
    try {
      const response = await fetch("/status", {cache: "no-store"});
      if (!response.ok) throw new Error("Status unavailable");
      update(await response.json());
    } catch (_) {
      progress.textContent = "Checking recordings is temporarily unavailable. Retrying…";
      window.setTimeout(poll, 5000);
    }
  }
  window.setTimeout(poll, main.dataset.validated === "true" || main.dataset.phase === "error" ? 5000 : 1000);
})();
