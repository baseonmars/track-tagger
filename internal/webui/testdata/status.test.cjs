const {test} = require('node:test');
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const {runInNewContext} = require('node:vm');
const {join} = require('node:path');
const code = readFileSync(join(__dirname, '../assets/status.js'), 'utf8');
const {ordered, preferences, savePreferences, storageKey} = require('../assets/status.js');

test('library orders exact statuses, timestamps, titles and ids', () => {
  const items = [
    {id:'z',title:'Beta',statusCode:'ready',lastModified:'100'},
    {id:'b',title:'alpha',statusCode:'needs-review',lastModified:'90'},
    {id:'a',title:'alpha',statusCode:'needs-review',lastModified:'90'},
    {id:'c',title:'Missing',statusCode:'completed',lastModified:'0'},
    {id:'d',title:'Old',statusCode:'playback-only',lastModified:'80'},
  ];
  const ids = p => ordered(items,p).map(x=>x.id).join(',');
  assert.equal(ids({filter:'all',sort:'modified',pin:true,group:false}),'a,b,z,d,c');
  assert.equal(ids({filter:'all',sort:'modified',pin:false,group:false}),'z,a,b,d,c');
  assert.equal(ids({filter:'ready',sort:'modified',pin:true,group:true}),'z');
  assert.equal(ids({filter:'all',sort:'alphabetical',pin:false,group:false}),'a,b,z,c,d');
  assert.equal(ids({filter:'all',sort:'modified',pin:false,group:true}),'a,b,z,c,d');
  assert.equal(ids({filter:'completed',sort:'modified',pin:false,group:false}),'c');
});

test('library preferences restore valid values and survive unavailable storage', () => {
  const values = new Map();
  const storage = {getItem:key=>values.get(key)||null,setItem:(key,value)=>values.set(key,value)};
  const selected = {filter:'completed',sort:'alphabetical',pin:false,group:true};
  savePreferences(storage, selected);
  assert.deepEqual(preferences(storage), selected);
  values.set(storageKey,'{"filter":"broken"}');
  assert.deepEqual(preferences(storage), {filter:'all',sort:'modified',pin:true,group:false});
  assert.deepEqual(preferences({getItem(){throw Error('denied')}}), {filter:'all',sort:'modified',pin:true,group:false});
  assert.doesNotThrow(()=>savePreferences({setItem(){throw Error('denied')}},selected));
});

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
