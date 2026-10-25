(() => {
  const defaults = {filter: "all", sort: "modified", pin: true, group: false};
  const groups = ["needs-review", "failed", "ready", "waiting", "checking", "completed", "other"];
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
      if (p && ["all", "needs-review", "ready", "completed", "waiting", "checking", "failed", "playback-only"].includes(p.filter) && ["modified", "alphabetical", "artist", "date", "tracks", "status", "issues"].includes(p.sort) && typeof p.pin === "boolean" && typeof p.group === "boolean") return p;
    } catch (_) {}
    return {...defaults};
  }
  function savePreferences(storage, p) { try { storage.setItem(storageKey, JSON.stringify(p)); } catch (_) {} }
  function groupCode(code) { return code === "loading" || code === "validating" ? "checking" : groups.includes(code) ? code : "other"; }
  function timestamp(value) { try { return BigInt(value || 0); } catch (_) { return 0n; } }
  function ordered(items, p) {
    return items.filter(item => (p.filter === "all" || (p.filter === "playback-only" ? item.playbackOnly || item.statusCode === "playback-only" : p.filter === "checking" ? groupCode(item.statusCode) === "checking" : item.statusCode === p.filter)) && (!p.search || [item.artist,item.title,item.relativePath].some(value => (value || "").toLocaleLowerCase().includes(p.search.toLocaleLowerCase())))).slice().sort((a, b) => {
      if (p.filter === "all") {
        if (p.group) { const diff = groups.indexOf(groupCode(a.statusCode)) - groups.indexOf(groupCode(b.statusCode)); if (diff) return diff; }
        else if (p.pin) { const diff = Number(b.statusCode === "needs-review") - Number(a.statusCode === "needs-review"); if (diff) return diff; }
      }
      let direction = p.direction === "desc" ? -1 : 1;
      if (p.sort === "modified" && p.direction !== "asc") direction = -1;
      if (p.sort === "modified") { const x = timestamp(a.lastModified), y = timestamp(b.lastModified); if ((x === 0n) !== (y === 0n)) return x === 0n ? 1 : -1; if (x !== y) return (x > y ? 1 : -1) * direction; }
      else if (["artist", "date", "tracks", "status", "issues"].includes(p.sort)) {
        const key = p.sort === "status" ? "statusCode" : p.sort;
        let x = p.sort === "issues" ? (a.issues || []).length : a[key], y = p.sort === "issues" ? (b.issues || []).length : b[key];
        if (p.sort === "date" && (!x || !y)) { if (!!x !== !!y) return x ? -1 : 1; }
        else { const diff = typeof x === "number" ? x - y : String(x || "").localeCompare(String(y || "")); if (diff) return diff * direction; }
      }
      if (p.sort === "alphabetical" && p.direction === "desc") return b.title.localeCompare(a.title) || a.id.localeCompare(b.id);
      return a.title.localeCompare(b.title) || a.id.localeCompare(b.id);
    });
  }
  if (typeof window !== "undefined") window.TrackTaggerLibrary = {ordered, preferences, savePreferences, groupCode};
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
  function update(status) {
    const selected = recordingId && (status.recordings || []).find(item => item.id === recordingId);
    const refreshReady = !!(selected && selected.ready && (main.dataset.recordingReady !== "true" || status.generation !== initialGeneration));
    progress.textContent = progressText(status, refreshReady);
    progress.hidden = status.validated && !refreshReady;
    if (library && library.updateCards && status.generation !== generation) { generation = status.generation; library.updateCards(status.recordings || []); }
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
