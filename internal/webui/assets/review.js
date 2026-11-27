"use strict";
(function () {
  function validDate(value) {
    if (!/^\d{4}(-\d{2})?(-\d{2})?$/.test(value)) return false;
    const parts = value.split("-").map(Number);
    if (parts[0] < 1) return false;
    if (parts.length > 1 && (parts[1] < 1 || parts[1] > 12)) return false;
    if (parts.length === 3) {
      const leap = parts[0] % 4 === 0 && (parts[0] % 100 !== 0 || parts[0] % 400 === 0);
      const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
      if (parts[2] < 1 || parts[2] > days[parts[1] - 1]) return false;
    }
    return true;
  }
  function actionState({ reviewed, writable, status, changed, approved, valid, submitting, validated = true }) {
    const saved = reviewed && !changed;
    const eligible = status === "ready" || status === "failed" ||
      (approved && changed && (status === "completed" || status === "needs-review")) ||
      (approved && status === "needs-review");
    const save = validated && approved && valid && !submitting;
    const apply = validated && valid && writable && eligible && (approved || saved) && !submitting;
    let help = "Check the approval box to save your review.";
    if (submitting) help = "Submitting review; please wait.";
    else if (!validated) help = "Library validation is still running. Refresh this recording when it completes before saving or applying.";
    else if (!valid) help = "Complete the required fields, a valid date or Date unknown, and unique positive disc/track numbers.";
    else if (!writable) help = "Preview mode: you can save an approved review. Restart in write or copy mode to apply it.";
    else if (status === "waiting") help = "Files are waiting to settle or become locally available. You can save a review; refresh before applying.";
    else if (status === "completed" && !changed) help = "This recording is already applied. Edit and approve a change to apply an updated review.";
    else if (changed && !approved) help = "Your edits need approval before you can save or apply them.";
    else if (saved && !approved) help = "Apply the unchanged saved review, or check approval to save a new review.";
    else if (approved) help = "Save your review, or save it and apply now.";
    return { save, apply, label: !approved && saved ? "Apply saved review" : "Save and apply", help };
  }
  // Match the portable component used by renameFilename in internal/app/rename.go.
  function proposedFilename(number, title) {
    const digits = String(number).replace(/^0+(?=\d)/, "");
    const clean = title
      .replace(/^[\x09-\x0d \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\x09-\x0d \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/g, "")
      .replace(/[\x00-\x1f<>:"/\\|?*]/g, "_");
    if (!/^[1-9]\d*$/.test(digits) || !clean) return "";
    const prefix = digits.padStart(2, "0") + " ";
    let remaining = 255 - prefix.length - ".flac".length;
    let fitting = "";
    for (const char of clean) {
      const point = char.codePointAt(0);
      remaining -= point < 0x80 ? 1 : point < 0x800 ? 2 : point < 0x10000 ? 3 : 4;
      if (remaining < 0) break;
      fitting += char;
    }
    return prefix + fitting + ".flac";
  }
  if (typeof module !== "undefined") module.exports = { actionState, validDate, proposedFilename };
  if (typeof document === "undefined") return;
  const form = document.getElementById("review-form");
  if (!form) return;
  const save = document.getElementById("save-review");
  const apply = document.getElementById("apply-review");
  const help = document.getElementById("review-help");
  const approve = form.elements.namedItem("approve");
  const notesSignature = form.elements.namedItem("notes_signature");
  const notesTarget = form.elements.namedItem("notes_target");
  function updateNotesPreview() {
    if (!notesSignature) return;
    document.getElementById("notes-signature-preview").hidden = !notesSignature.checked;
    const selected = notesTarget?.selectedOptions[0];
    const target = document.getElementById("notes-preview-target");
    const addition = document.getElementById("notes-preview-addition");
    const reason = document.getElementById("notes-preview-reason");
    const addedText = selected?.dataset.addition || "";
    target.hidden = addition.hidden = !addedText;
    target.querySelector("code").textContent = selected?.dataset.target || "";
    addition.textContent = addedText;
    reason.textContent = selected?.dataset.reason ||
      (notesTarget ? (selected?.value ? "" : "Choose which show-notes file to edit.") : "No local show-notes file is available.");
  }
  const fields = Array.from(form.elements).filter(el => el.name && !["csrf", "id", "fingerprint", "approve"].includes(el.name));
  const rename = form.elements.namedItem("rename_files");
  const renameNotice = document.getElementById("rename-notice");
  const titles = fields.filter(el => el.name === "track_title");
  const discs = fields.filter(el => el.name === "track_disc");
  const numbers = fields.filter(el => el.name === "track_number");
  const skips = fields.filter(el => el.name === "track_skip");
  const files = fields.filter(el => el.name === "track_file");
  function snapshot() { return JSON.stringify(fields.map(el => el.type === "checkbox" ? el.checked : el.value)); }
  const previewMD5 = document.getElementById("preview-md5");
  const md5Result = document.getElementById("md5-preview");
  if (previewMD5) {
    let request = 0;
    const clear = () => { request++; md5Result.textContent = ""; };
    form.addEventListener("input", clear);
    form.addEventListener("change", clear);
    const preview = async () => {
      const current = ++request;
      md5Result.textContent = "Checking whole-file checksums…";
      try {
        const response = await fetch("/preview-md5", { method: "POST", body: new URLSearchParams(new FormData(form)) });
        const message = await response.text();
        if (current === request) md5Result.textContent = message;
      } catch {
        if (current === request) md5Result.textContent = "Could not load the checksum preview. Try again.";
      }
    };
    previewMD5.addEventListener("click", preview);
    form.elements.namedItem("cleanup_md5").addEventListener("change", event => {
      if (event.target.checked) queueMicrotask(preview);
    });
  }
  const baseline = snapshot();
  let submitting = false;
  let navigationApproved = false;
  function valid() {
    const value = name => form.elements.namedItem(name).value.trim();
    const date = value("date");
    if (form.elements.namedItem("date_unknown").checked ? date !== "" : !validDate(date)) return false;
    if (!value("artist") || !value("album")) return false;
    const keys = new Set();
    for (let i = 0; i < discs.length; i++) {
      if (skips[i]?.checked) continue;
      if (!/^\d+$/.test(discs[i].value) || !/^\d+$/.test(numbers[i].value) || !titles[i].value.trim()) return false;
      const disc = BigInt(discs[i].value);
      const number = BigInt(numbers[i].value);
      if (disc < 1 || number < 1 || disc > 9223372036854775807n || number > 9223372036854775807n) return false;
      const key = disc + "/" + number;
      if (keys.has(key)) return false;
      keys.add(key);
    }
    return keys.size > 0 && form.checkValidity();
  }
  function update() {
    updateNotesPreview();
    let movesFile = false;
    skips.forEach((el, i) => {
      for (const field of [titles[i], discs[i], numbers[i]]) {
        field.required = !el.checked;
        field.readOnly = el.checked;
      }
      const name = files[i].closest("td");
      const preview = name.querySelector(".proposed-filename");
      const original = preview.dataset.original;
      const proposed = el.checked ? original : proposedFilename(numbers[i].value, titles[i].value);
      preview.hidden = !rename.checked;
      preview.textContent = proposed ? "Proposed: " + proposed : "Enter a track number and title";
      movesFile ||= Boolean(proposed && proposed !== original);
    });
    const renameBlocked = renameNotice && rename.checked && movesFile;
    if (renameNotice) renameNotice.hidden = !renameBlocked;
    const state = actionState({ reviewed: form.dataset.reviewed === "true", writable: form.dataset.writable === "true", status: form.dataset.status, changed: snapshot() !== baseline, approved: approve.checked, valid: valid(), submitting, validated: form.dataset.validated !== "false" });
    if (renameBlocked) {
      state.apply = false;
      state.help = renameNotice.textContent;
    }
    save.disabled = !state.save;
    if (apply) { apply.disabled = !state.apply; apply.textContent = state.label; }
    help.textContent = state.help;
    return state;
  }
  function edited(event = {}) {
    if (event.target !== approve && fields.includes(event.target)) approve.checked = false;
    update();
  }
  form.addEventListener("input", edited);
  form.addEventListener("change", edited);
  window.addEventListener("beforeunload", event => {
    if (!submitting && !navigationApproved && snapshot() !== baseline) {
      event.preventDefault();
      event.returnValue = "";
    }
  });
  document.addEventListener("click", event => {
    const link = event.target.closest("a[href]");
    if (!link || event.defaultPrevented || event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || link.target === "_blank" || link.hasAttribute("download")) return;
    const destination = new URL(link.href, window.location.href);
    if (destination.origin === window.location.origin && destination.pathname === window.location.pathname && destination.search === window.location.search) return;
    if (submitting || snapshot() === baseline) return;
    if (!window.confirm("Leave this review and discard your unsaved changes?")) event.preventDefault();
    else navigationApproved = true;
  });
  form.addEventListener("submit", event => {
    const state = update();
    const applying = apply && event.submitter === apply;
    if (!(applying ? state.apply : state.save)) { event.preventDefault(); return; }
    form.action = applying ? "/apply" : "/review";
    submitting = true;
    update();
  });
  window.addEventListener("pageshow", () => { submitting = false; navigationApproved = false; update(); });
  update();
})();
