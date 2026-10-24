const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { runInNewContext } = require('node:vm');
require('./status.test.cjs');
const code = readFileSync(require('node:path').join(__dirname, '../assets/player.js'), 'utf8');
function setup() {
  const element = () => ({
    events: {}, attributes: {}, disabled: true, textContent: '', hidden: true,
    addEventListener(name, callback) { this.events[name] = callback; },
    setAttribute(name, value) { this.attributes[name] = value; },
    getAttribute(name) { return this.attributes[name]; },
    removeAttribute(name) { delete this.attributes[name]; },
    emit(name) { this.events[name]?.(); },
  });
  const audio = Object.assign(element(), {
    paused: true, ended: false, error: null, loads: 0, plays: 0,
    load() { this.loads++; this.error = null; },
    play() { this.plays++; this.paused = false; this.emit('play'); return Promise.resolve(); },
    pause() { this.paused = true; this.emit('pause'); },
  });
  Object.defineProperty(audio, 'src', {set(value) {this.attributes.src = value;}});
  const buttons = [0, 1].map(index => Object.assign(element(), {
    dataset: {audioUrl:`/audio?track=${index}`, title:`Track ${index}`},
    closest() {return {querySelector() {return {value: `Edited ${index}`};}};},
  }));
  const nodes = {'recording-player':audio,'player-title':element(),'player-error':element(),'previous-track':element(),'next-track':element()};
  runInNewContext(code, {document:{getElementById:id=>nodes[id],querySelectorAll:()=>buttons}});
  return {audio, buttons, nodes};
}
test('initialization never loads or starts music', () => {
  const {audio, buttons, nodes} = setup();
  assert.equal(audio.loads, 0); assert.equal(audio.plays, 0); assert.equal(audio.getAttribute('src'), undefined);
  assert.ok(buttons.every(button => !button.disabled));
  assert.ok(nodes['previous-track'].disabled); assert.ok(nodes['next-track'].disabled);
});
test('explicit play, pause, next, previous, and end keep a single controlled player', () => {
  const {audio, buttons, nodes} = setup();
  buttons[0].emit('click');
  assert.equal(audio.getAttribute('src'), '/audio?track=0'); assert.equal(audio.plays, 1);
  assert.equal(nodes['player-title'].textContent, 'Selected track: Edited 0');
  assert.equal(buttons[0].textContent, 'Pause'); assert.ok(nodes['previous-track'].disabled);
  buttons[0].emit('click'); assert.ok(audio.paused);
  buttons[0].emit('click'); assert.equal(audio.loads, 1);
  nodes['next-track'].emit('click'); assert.equal(audio.getAttribute('src'), '/audio?track=1');
  assert.ok(nodes['next-track'].disabled); assert.equal(buttons[0].getAttribute('aria-current'), undefined);
  const plays = audio.plays; audio.ended = true; audio.paused = true; audio.emit('ended');
  assert.equal(audio.plays, plays); assert.equal(buttons[1].textContent, 'Play');
  nodes['previous-track'].emit('click'); assert.equal(audio.getAttribute('src'), '/audio?track=0');
});
test('unavailable media shows actionable error and can retry', () => {
  const {audio, buttons, nodes} = setup();
  buttons[0].emit('click'); audio.paused = true; audio.error = {}; audio.emit('error');
  assert.equal(nodes['player-error'].hidden, false);
  assert.match(nodes['player-error'].textContent, /Choose Play to retry/);
  buttons[0].emit('click'); assert.equal(audio.loads, 2); assert.equal(nodes['player-error'].hidden, true);
});
