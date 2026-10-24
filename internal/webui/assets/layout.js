(() => {
  let layout = "compact";
  try { if (localStorage.getItem("track-tagger.layout") === "classic") layout = "classic"; } catch (_) {}
  document.documentElement.dataset.layout = layout;
  document.addEventListener("DOMContentLoaded", () => {
    const choice = document.getElementById("layout-choice");
    if (!choice) return;
    choice.value = layout;
    choice.addEventListener("change", () => {
      layout = choice.value === "classic" ? "classic" : "compact";
      document.documentElement.dataset.layout = layout;
      let message = "Layout saved for this browser.";
      try { localStorage.setItem("track-tagger.layout", layout); } catch (_) { message = "Layout changed for this page. Browser storage is unavailable."; }
      document.getElementById("layout-feedback").textContent = message;
    });
  });
})();
