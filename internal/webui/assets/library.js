(() => {
  const pageSize = 100;
  function pageItems(items, page) { return items.slice((page - 1) * pageSize, page * pageSize); }
  if (typeof module !== "undefined") module.exports = {pageItems, pageSize};
  if (typeof document === "undefined") return;
  document.addEventListener("DOMContentLoaded", () => {
    const data = document.getElementById("library-data"), library = document.getElementById("recording-library");
    if (!data || !library) return;
    const api = window.TrackTaggerLibrary;
    let items = JSON.parse(data.textContent) || [];
    let storage; try { storage = window.localStorage; } catch (_) {}
    const p = api.preferences(storage);
    let state = {page: 1, search: "", scroll: 0};
    try { const saved = JSON.parse(sessionStorage.getItem("track-tagger.library-view")); if (saved) state = {...state,...saved}; } catch (_) {}
    p.search = typeof state.search === "string" ? state.search : "";
    p.direction = p.direction === "asc" ? "asc" : p.direction === "desc" ? "desc" : p.sort === "modified" ? "desc" : "asc";
    const controls = Object.fromEntries(["filter","sort","pin","group","search","direction"].map(key => [key,document.getElementById("recording-" + key)]));
    for (const key of ["filter","sort","search"]) controls[key].value = p[key];
    controls.pin.checked = p.pin; controls.group.checked = p.group;
    const pagination = document.getElementById("library-pagination");
    function el(tag, cls, text) { const n = document.createElement(tag); if (cls) n.className = cls; if (text !== undefined) n.textContent = text; return n; }
    function link(item) { const n = el("a","",item.title); n.href = "/recording?" + new URLSearchParams({id:item.id}); n.dataset.focus = "recording-" + item.id; return n; }
    function modified(item) { const millis = Number(BigInt(item.lastModified || 0) / 1000000n); return millis ? new Date(millis).toLocaleString() : "Unknown"; }
    function persist() { try { sessionStorage.setItem("track-tagger.library-view", JSON.stringify({...state,search:p.search,scroll:window.scrollY,gridScroll:library.scrollLeft,gridTop:library.scrollTop})); } catch (_) {} }
    function card(item) {
      const n = el("article","recording-card"), body = el("div","recording-main");
      body.append(el("span","status",item.status)); const h = el("h2"); h.append(link(item)); body.append(h,el("p","count",`${item.tracks} audio files${item.date ? " | " + item.date : ""}`));
      const issues = el("ul","issue-summary"); for (const issue of item.issues || []) issues.append(el("li","",issue)); body.append(issues); const action = link(item); action.className="button"; action.textContent=item.action + ""; n.append(body,action); return n;
    }
    function render() {
      const focus = document.activeElement && document.activeElement.dataset.focus, left = library.scrollLeft, top = library.scrollTop;
      const ordered = api.ordered(items,p), pages = Math.max(1, Math.ceil(ordered.length/pageSize));
      state.page = Math.min(pages,Math.max(1,Number(state.page) || 1));
      controls.pin.parentElement.hidden = controls.group.parentElement.hidden = p.filter !== "all";
      controls.pin.disabled = p.filter !== "all" || p.group; controls.group.disabled = p.filter !== "all";
      controls.direction.textContent = p.direction === "asc" ? "Ascending" : "Descending";
      const visible = pageItems(ordered,state.page), compact = document.documentElement.dataset.layout !== "classic";
      const output = [];
      if (compact) {
        const table = el("table","library-table"), head = el("thead"), header = el("tr");
        for (const [label,key] of [["Status","status"],["Artist","artist"],["Recording","alphabetical"],["Date","date"],["Tracks","tracks"],["Last modified","modified"],["Issues","issues"]]) {
          const th = el("th"), button = el("button","column-sort",label);
          button.dataset.focus="sort-"+key; th.scope="col"; th.setAttribute("aria-sort",p.sort === key ? p.direction === "asc" ? "ascending" : "descending" : "none");
          button.addEventListener("click", () => { p.direction = p.sort === key && p.direction === "asc" ? "desc" : "asc"; p.sort=key; controls.sort.value=key; changed(); }); th.append(button); header.append(th);
        }
        head.append(header); const body=el("tbody"); let previous="";
        for (const item of visible) {
          const code = api.groupCode(item.statusCode);
          if (p.group && p.filter === "all" && code !== previous) { const tr=el("tr","library-group"), td=el("th","",code === "other" ? "Other recordings" : item.status); td.colSpan=7; tr.append(td); body.append(tr); previous=code; }
          const row=el("tr"); row.dataset.id=item.id;
          for (const value of [item.status,item.artist || "Unknown",null,item.date || "Unknown",String(item.tracks),modified(item),(item.issues || []).join(" | ")]) { const td=el("td"); if(value === null) {td.append(link(item)); td.title=item.relativePath || "";} else {td.textContent=value;td.title=value;} row.append(td); }
          const cell = row.lastElementChild, details = el("details","row-details"), summary = el("summary","",`${(item.issues || []).length} checks`);
          details.dataset.focus="details-"+item.id; summary.dataset.focus="summary-"+item.id;
          details.append(summary,el("p","",item.title),el("p","",item.artist || "Artist unknown"),el("p","",item.relativePath || "Folder unavailable"));
          for(const issue of item.issues || []) details.append(el("p","",issue));
          cell.replaceChildren(details);
          body.append(row);
        }
        table.append(head,body); output.push(table);
      } else { let previous=""; for(const item of visible) { if(p.group && p.filter === "all" && api.groupCode(item.statusCode) !== previous) { output.push(el("h2","library-group-heading",api.groupCode(item.statusCode) === "other" ? "Other recordings" : item.status)); previous=api.groupCode(item.statusCode); } output.push(card(item)); } }
      if (!visible.length) output.push(el("p","library-empty",items.length ? "No recordings match this filter" : "No recordings found yet"));
      library.replaceChildren(...output);
      const prev=el("button","","Previous"), next=el("button","","Next"); prev.dataset.focus="previous";next.dataset.focus="next";prev.disabled=state.page<=1;next.disabled=state.page>=pages;
      prev.addEventListener("click",()=>{state.page--;render();persist();});next.addEventListener("click",()=>{state.page++;render();persist();});
      pagination.replaceChildren(prev,el("span","",`Page ${state.page} of ${pages} | ${ordered.length} recordings`),next);
      library.scrollLeft=left;library.scrollTop=top;
      if(focus) { const node = [...library.querySelectorAll("[data-focus]"),...pagination.querySelectorAll("[data-focus]")].find(n=>n.dataset.focus===focus); if(node) node.focus({preventScroll:true}); }
    }
    function changed() {state.page=1;api.savePreferences(storage,p);render();persist();}
    for (const key of ["filter","sort","pin","group"]) controls[key].addEventListener("change",()=>{p[key] = ["pin","group"].includes(key) ? controls[key].checked : controls[key].value;if(key === "sort") p.direction=p.sort === "modified" ? "desc" : "asc";changed();});
    controls.search.addEventListener("input",()=>{p.search=controls.search.value;changed();});
    controls.direction.addEventListener("click",()=>{p.direction=p.direction === "asc" ? "desc" : "asc";changed();});
    library.updateCards = updated => {items=updated;render();};
    library.addEventListener("click",persist);window.addEventListener("pagehide",persist);
    render(); library.scrollLeft=state.gridScroll || 0;library.scrollTop=state.gridTop || 0; if(state.scroll) window.scrollTo(0,state.scroll);
  });
})();
