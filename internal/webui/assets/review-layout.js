"use strict";
(function () {
  if (document.documentElement.dataset.layout !== "compact") return;
  const tracks = document.getElementById("track-list");
  if (!tracks) return;
  document.body.classList.add("compact-review");
  const heading = document.querySelector(".heading");
  const back = document.querySelector(".back");
  if (heading && back) heading.prepend(back);
  const dateSearch = document.querySelector("a[referrerpolicy='no-referrer']");
  if (dateSearch && heading) {
    dateSearch.classList.add("date-search");
    const paragraph = dateSearch.parentElement;
    heading.append(dateSearch);
    if (paragraph.tagName === "P") paragraph.remove();
  }
  const form = document.getElementById("review-form");
  const player = document.querySelector(".listening");
  const notes = document.querySelector(".notes");
  const workspace = document.createElement("div");
  workspace.className = "review-workspace";
  (form || tracks).before(workspace);
  const sidebar = document.createElement("div");
  sidebar.className = "review-sidebar";
  const content = document.createElement("div");
  content.className = "review-content";
  workspace.append(sidebar, content);
  if (!form) {
    workspace.classList.add("playback-workspace");
    if (!notes) { sidebar.remove(); workspace.classList.add("without-sidebar"); }
    const explanation = tracks.querySelector("p");
    if (explanation) explanation.classList.add("playback-explanation");
  }
  // Keep the original form and every named control intact.
  if (form) {
    workspace.before(form);
    form.append(workspace);
    const sections = Array.from(form.children).filter(node => node.tagName === "SECTION");
    const album = sections[0];
    const actions = sections[sections.length - 1];
    album.id = "album-panel";
    sidebar.append(album);
    actions.classList.add("review-approval");
    form.append(actions);
  }
  if (notes) {
    sidebar.append(notes);
    notes.id = "notes-panel";
    notes.querySelector("details").open = true;
  }
  if (form && notes) {
    const tabs = document.createElement("div");
    tabs.className = "review-tabs";
    tabs.setAttribute("role", "tablist");
    tabs.setAttribute("aria-label", "Recording information");
    const panels = [document.getElementById("album-panel"), notes];
    const buttons = panels.map((panel, i) => {
      const button = document.createElement("button");
      button.type = "button";
      button.id = panel.id + "-tab";
      button.textContent = i ? "Notes" : "Album";
      button.setAttribute("role", "tab");
      button.setAttribute("aria-controls", panel.id);
      panel.setAttribute("role", "tabpanel");
      panel.setAttribute("aria-labelledby", button.id);
      tabs.append(button);
      button.addEventListener("click", () => select(i));
      button.addEventListener("keydown", event => {
        let next;
        if (event.key === "ArrowRight" || event.key === "ArrowLeft") next = 1 - i;
        if (event.key === "Home") next = 0;
        if (event.key === "End") next = 1;
        if (next !== undefined) { event.preventDefault(); select(next); buttons[next].focus(); }
      });
      return button;
    });
    function select(index) {
      panels.forEach((panel, i) => { panel.hidden = i !== index; buttons[i].setAttribute("aria-selected", String(i === index)); buttons[i].tabIndex = i === index ? 0 : -1; });
    }
    sidebar.prepend(tabs);
    select(0);
  }
  content.append(tracks);
  if (player) content.append(player);
  const issue = Array.from(document.querySelectorAll(".attention")).find(el => el.querySelector("h2"));
  if (issue) {
    const details = document.createElement("details");
    const summary = document.createElement("summary");
    summary.textContent = "What needs checking — show all issues";
    while (issue.firstChild) details.append(issue.firstChild);
    details.prepend(summary);
    issue.append(details);
    const diagnostics = document.querySelector(".technical ul");
    if (diagnostics) {
      const label = document.createElement("h3"); label.textContent = "All reasons";
      details.append(label, diagnostics.cloneNode(true));
    }
    content.prepend(issue);
    issue.addEventListener("click", event => {
      const link = event.target.closest("a[href^='#track-']");
      if (!link) return;
      const row = document.getElementById(link.hash.slice(1));
      const input = row && row.querySelector("input:not([type=hidden])");
      if (input) { event.preventDefault(); input.focus(); input.scrollIntoView({block:"nearest"}); }
    });
  }
  const table = tracks.querySelector(".mapping");
  if (table) {
    const head = table.tHead.rows[0];
    const playHead = document.createElement("th"); playHead.textContent = "Play"; head.prepend(playHead);
    head.cells[1].textContent = "File";
    head.cells[4].textContent = "Title";
    const artistHead = document.createElement("th"); artistHead.textContent = "Artist override"; head.append(artistHead);
    Array.from(table.tBodies[0].rows).forEach(row => {
      const filename = row.querySelector(".filename");
      const source = row.querySelector("input[name=track_file]");
      if (filename && source) filename.title = source.value;
      const playCell = document.createElement("td"); playCell.className = "track-play";
      const play = row.querySelector(".play-track"); if (play) playCell.append(play);
      row.prepend(playCell);
      const artistCell = document.createElement("td"); artistCell.className = "artist-override";
      const artist = row.querySelector(".track-artist");
      if (artist) artistCell.append(artist);
      row.append(artistCell);
    });
  }
  const listeningTracks = tracks.querySelector(".listening-tracks");
  if (listeningTracks) {
    const table = document.createElement("table");
    table.className = "playback-table";
    const head = table.createTHead().insertRow();
    ["Play", "File", "Title"].forEach(text => { const th = document.createElement("th"); th.textContent = text; head.append(th); });
    const body = table.createTBody();
    Array.from(listeningTracks.children).forEach(item => {
      const row = body.insertRow();
      const play = item.querySelector(".play-track"); if (play) row.insertCell().append(play); else row.insertCell();
      const filename = item.querySelector(".count"); if (filename) row.insertCell().append(filename); else row.insertCell();
      const title = item.querySelector("strong"); if (title) row.insertCell().append(title); else row.insertCell();
    });
    const scroll = document.createElement("div"); scroll.className = "table-scroll";
    scroll.append(table); listeningTracks.replaceWith(scroll);
  }
})();
