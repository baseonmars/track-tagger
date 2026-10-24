(() => {
  const defaults = {filter: "all", sort: "modified", pin: true, group: false};
  const groups = ["needs-review", "failed", "ready", "waiting", "checking", "completed", "other"];
  const labels = {"needs-review": "Needs review", failed: "Could not finish", ready: "Ready", waiting: "Waiting for files", checking: "Checking files", completed: "Completed", other: "Other recordings"};
  const storageKey = "track-tagger.recording-library.preferences";
  function progressText(status, refreshReady) {
    if (status.phase === "error") return "Could not validate the library. " + (status.error || "Refresh to retry.");
    if (refreshReady) return "This recording is ready. Refresh it to review the latest details.";
    if (status.validated) return "";
    return `Checking recordings: ${status.scanned} of ${status.total}. Review actions become available after validation.`;
  }
  function preferences(storage) {
    try {
      const p = JSON.parse(storage.getItem(storageKey));
      if (p && ["all", "needs-review", "ready", "completed"].includes(p.filter) && ["modified", "alphabetical"].includes(p.sort) && typeof p.pin === "boolean" && typeof p.group === "boolean") return p;
    } catch (_) {}
    return {...defaults};
  }
  function savePreferences(storage, p) { try { storage.setItem(storageKey, JSON.stringify(p)); } catch (_) {} }
  function groupCode(code) { return code === "loading" || code === "validating" ? "checking" : groups.includes(code) ? code : "other"; }
  function timestamp(value) { try { return BigInt(value || 0); } catch (_) { return 0n; } }
  function ordered(items, p) {
    return items.filter(item => p.filter === "all" || item.statusCode === p.filter).slice().sort((a, b) => {
      if (p.filter === "all") {
        if (p.group) { const diff = groups.indexOf(groupCode(a.statusCode)) - groups.indexOf(groupCode(b.statusCode)); if (diff) return diff; }
        else if (p.pin) { const diff = Number(b.statusCode === "needs-review") - Number(a.statusCode === "needs-review"); if (diff) return diff; }
      }
      if (p.sort === "modified") { const x = timestamp(a.lastModified), y = timestamp(b.lastModified); if (x !== y) return x > y ? -1 : 1; }
      return a.title.localeCompare(b.title) || a.id.localeCompare(b.id);
    });
  }
  if (typeof module !== "undefined") module.exports = {progressText, preferences, savePreferences, groupCode, ordered, storageKey};
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
    article.dataset.id = item.id;
    article.dataset.title = item.title;
    article.dataset.statusCode = item.statusCode;
    article.dataset.lastModified = String(item.lastModified || 0);
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
  if (library) {
    const filter = document.getElementById("recording-filter");
    const sort = document.getElementById("recording-sort");
    const pin = document.getElementById("recording-pin");
    const group = document.getElementById("recording-group");
    const storage = (() => { try { return window.localStorage; } catch (_) { return null; } })();
    const p = preferences(storage);
    filter.value = p.filter; sort.value = p.sort; pin.checked = p.pin; group.checked = p.group;
    let nodes = Array.from(library.children).filter(node => node.className === "recording-card");
    function render() {
      pin.parentElement.hidden = p.filter !== "all";
      group.parentElement.hidden = p.filter !== "all";
      pin.disabled = p.filter !== "all" || p.group;
      group.disabled = p.filter !== "all";
      const cards = ordered(nodes.map(node => ({id: node.dataset.id, title: node.dataset.title, statusCode: node.dataset.statusCode, lastModified: node.dataset.lastModified, node})), p);
      if (!cards.length) {
        const empty = element("section", "library-empty");
        empty.append(element("h2", "", nodes.length ? "No recordings match this filter" : "No recordings found yet"));
        empty.append(element("p", "", nodes.length ? "Choose another filter to see more recordings." : "Keep this server running while local files arrive, then refresh this page."));
        library.replaceChildren(empty);
        return;
      }
      const output = [];
      let previous = "";
      for (const item of cards) {
        const current = groupCode(item.statusCode);
        if (p.filter === "all" && p.group && current !== previous) {
          output.push(element("h2", "library-group-heading", labels[current]));
          previous = current;
        }
        output.push(item.node);
      }
      library.replaceChildren(...output);
    }
    for (const [control, key, field] of [[filter, "filter", "value"], [sort, "sort", "value"], [pin, "pin", "checked"], [group, "group", "checked"]])
      control.addEventListener("change", () => { p[key] = control[field]; savePreferences(storage, p); render(); });
    render();
    library.updateCards = items => { nodes = items.map(card); render(); };
  }
  function update(status) {
    const selected = recordingId && (status.recordings || []).find(item => item.id === recordingId);
    const refreshReady = !!(selected && selected.ready && (main.dataset.recordingReady !== "true" || status.generation !== initialGeneration));
    progress.textContent = progressText(status, refreshReady);
    progress.hidden = status.validated && !refreshReady;
    if (library && status.generation !== generation) { generation = status.generation; library.updateCards(status.recordings || []); }
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
