const {test} = require('node:test');
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const {runInNewContext} = require('node:vm');
const {join} = require('node:path');
const code = readFileSync(join(__dirname, '../assets/status.js'), 'utf8');

test('progress poll leaves a detail form alone and slows after validation', async () => {
  const timers = [];
  const form = {unchanged: 'user edits'};
  const progress = {textContent: '', hidden: false};
  const main = {dataset: {phase: 'validating', validated: 'false', generation:'0', recordingId:'one', recordingReady:'false'}};
  const document = {
    querySelector: () => main,
    getElementById: id => ({'scan-progress': progress, 'recording-library': null, 'review-form': form})[id],
  };
  let requested = 0;
  const window = {setTimeout: (fn, delay) => { timers.push({fn, delay}); }};
  const fetch = async (url, options) => {
    assert.equal(url, '/status'); assert.equal(options.cache, 'no-store');
    requested++;
    return {ok: true, json: async () => requested === 1
      ? {phase:'validating', validated:false, scanned:1, total:2}
      : {phase:'ready', validated:true, scanned:2, total:2, recordings:[{id:'one',ready:true}]}};
  };
  runInNewContext(code, {document, window, fetch});
  assert.equal(timers.length, 1);
  assert.equal(timers[0].delay,1000);
  await timers.shift().fn();
  assert.match(progress.textContent, /1 of 2/);
  assert.equal(timers.length, 1);
  assert.equal(timers[0].delay,1000);
  await timers.shift().fn();
  assert.match(progress.textContent, /Refresh it to review/);
  assert.equal(timers.length, 1);
  assert.equal(timers[0].delay, 5000);
  assert.equal(form.unchanged, 'user edits');
});

test('ready recording prompts refresh before the rest of the library finishes', async () => {
  const timers=[];
  const progress={textContent:'',hidden:false};
  const main={dataset:{phase:'validating',validated:'false',generation:'0',recordingId:'one',recordingReady:'false'}};
  const document={querySelector:()=>main,getElementById:id=>({'scan-progress':progress})[id]||null};
  const fetch=async()=>({ok:true,json:async()=>({phase:'validating',validated:false,generation:1,scanned:1,total:4,recordings:[{id:'one',ready:true}]})});
  runInNewContext(code,{document,window:{setTimeout:(fn,delay)=>timers.push({fn,delay})},fetch});
  await timers.shift().fn();
  assert.match(progress.textContent,/This recording is ready/);
  assert.equal(progress.hidden,false);
  assert.equal(timers[0].delay,1000);
});

test('history page does not poll or ask to refresh a recording',()=>{
  let timers=0;
  const document={querySelector:()=>({dataset:{phase:'validating',validated:'false'}}),getElementById:()=>null};
  runInNewContext(code,{document,window:{setTimeout:()=>timers++}});
  assert.equal(timers,0);
});

test('library poll clears removed recordings and renders new cards', async () => {
  const timers = [];
  const make = tag => ({tag, children:[], append(...nodes){this.children.push(...nodes);}, replaceChildren(...nodes){this.children=nodes;}});
  const library = make('div');
  const progress = {textContent:'', hidden:false};
  const document = {
    querySelector: () => ({dataset:{phase:'validating',validated:'false',generation:'0'}}),
    getElementById: id => ({'scan-progress':progress,'recording-library':library})[id] || null,
    createElement: make,
  };
  let calls=0;
  const fetch = async () => ({ok:true,json:async () => ++calls===1
    ? {phase:'validating',validated:false,generation:1,recordings:[],scanned:0,total:1}
    : {phase:'ready',validated:true,generation:2,recordings:[{id:'id & one',title:'Album <safe>',status:'Ready',action:'View preview',date:'1970',tracks:1,issues:['Check title']}],scanned:1,total:1}});
  runInNewContext(code,{document,window:{setTimeout:(fn,delay)=>timers.push({fn,delay})},fetch,URLSearchParams});
  await timers.shift().fn();
  assert.equal(library.children.length,1);
  assert.equal(library.children[0].tag,'section');
  await timers.shift().fn();
  assert.equal(library.children.length,1);
  assert.equal(library.children[0].tag,'article');
  const headingLink=library.children[0].children[0].children[1].children[0];
  assert.equal(headingLink.textContent,'Album <safe>');
  assert.equal(headingLink.href,'/recording?id=id+%26+one');
  assert.equal(timers[0].delay,5000);
});
