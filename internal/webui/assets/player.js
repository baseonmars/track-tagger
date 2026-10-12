(() => {
  'use strict';
  const audio = document.getElementById('recording-player');
  if (!audio) return;
  const buttons = Array.from(document.querySelectorAll('.play-track'));
  const title = document.getElementById('player-title');
  const error = document.getElementById('player-error');
  const previous = document.getElementById('previous-track');
  const next = document.getElementById('next-track');
  let current = -1;
  let generation = 0;

  function update() {
    buttons.forEach((button, index) => {
      const active = index === current;
      button.textContent = active && !audio.paused && !audio.ended ? 'Pause' : 'Play';
      if (active) button.setAttribute('aria-current', 'true');
      else button.removeAttribute('aria-current');
      const filename = button.dataset.title;
      button.setAttribute('aria-label', `${button.textContent} ${filename}`);
    });
    previous.disabled = current <= 0;
    next.disabled = current < 0 || current >= buttons.length - 1;
  }

  function showError() {
    error.textContent = 'This track could not be played. Your browser may not support its format, or the local file may be unavailable or changed. Choose Play to retry, or refresh the page if the file changed.';
    error.hidden = false;
    update();
  }

  function play(index) {
    const button = buttons[index];
    if (!button) return;
    const attempt = ++generation;
    error.hidden = true;
    const changed = current !== index;
    current = index;
    const editedTitle = button.closest('tr')?.querySelector('[name="track_title"]')?.value;
    title.textContent = `Selected track: ${editedTitle || button.dataset.title}`;
    if (changed || audio.error || !audio.getAttribute('src')) {
      audio.src = button.dataset.audioUrl;
      audio.load();
    }
    if (audio.ended) audio.currentTime = 0;
    update();
    audio.play().catch(() => {
      // Switching tracks can reject the previous pending play request.
      if (attempt === generation) showError();
    });
  }

  buttons.forEach((button, index) => {
    button.disabled = false;
    button.addEventListener('click', () => {
      if (index === current && !audio.paused && !audio.ended) {
        generation++;
        audio.pause();
      } else play(index);
    });
  });
  previous.addEventListener('click', () => play(current - 1));
  next.addEventListener('click', () => play(current + 1));
  ['play', 'pause', 'ended'].forEach(event => audio.addEventListener(event, update));
  audio.addEventListener('error', showError);
  update();
})();
