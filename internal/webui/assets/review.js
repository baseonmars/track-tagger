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
  if (typeof module !== "undefined") module.exports = { actionState, validDate };
  if (typeof document === "undefined") return;
  const form = document.getElementById("review-form");
  if (!form) return;
  const save = document.getElementById("save-review");
  const apply = document.getElementById("apply-review");
  const help = document.getElementById("review-help");
  const approve = form.elements.namedItem("approve");
  const fields = Array.from(form.elements).filter(el => el.name && !["csrf", "id", "fingerprint", "approve"].includes(el.name));
  function snapshot() { return JSON.stringify(fields.map(el => el.type === "checkbox" ? el.checked : el.value)); }
  const baseline = snapshot();
  let submitting = false;
  function valid() {
    const value = name => form.elements.namedItem(name).value.trim();
    const date = value("date");
    if (form.elements.namedItem("date_unknown").checked ? date !== "" : !validDate(date)) return false;
    if (!value("artist") || !value("album")) return false;
    const titles = fields.filter(el => el.name === "track_title");
    if (!titles.length || titles.some(el => !el.value.trim())) return false;
    const discs = fields.filter(el => el.name === "track_disc");
    const numbers = fields.filter(el => el.name === "track_number");
    const keys = new Set();
    for (let i = 0; i < discs.length; i++) {
      if (!/^\d+$/.test(discs[i].value) || !/^\d+$/.test(numbers[i].value) || Number(discs[i].value) < 1 || Number(numbers[i].value) < 1) return false;
      const key = Number(discs[i].value) + "/" + Number(numbers[i].value);
      if (keys.has(key)) return false;
      keys.add(key);
    }
    return form.checkValidity();
  }
  function update() {
    const state = actionState({ reviewed: form.dataset.reviewed === "true", writable: form.dataset.writable === "true", status: form.dataset.status, changed: snapshot() !== baseline, approved: approve.checked, valid: valid(), submitting, validated: form.dataset.validated !== "false" });
    save.disabled = !state.save;
    if (apply) { apply.disabled = !state.apply; apply.textContent = state.label; }
    help.textContent = state.help;
    return state;
  }
  form.addEventListener("input", update);
  form.addEventListener("change", update);
  form.addEventListener("submit", event => {
    const state = update();
    const applying = apply && event.submitter === apply;
    if (!(applying ? state.apply : state.save)) { event.preventDefault(); return; }
    form.action = applying ? "/apply" : "/review";
    submitting = true;
    update();
  });
  window.addEventListener("pageshow", () => { submitting = false; update(); });
  update();
})();
