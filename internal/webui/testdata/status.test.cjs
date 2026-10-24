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

test('initial cards, controls, and live status updates share saved preferences', async () => {
  const timers=[];
  const make = (tag, data={}) => ({tag,className:'',dataset:data,children:[],append(...nodes){this.children.push(...nodes);},replaceChildren(...nodes){this.children=nodes;}});
  const library=make('div');
  const first=make('article',{id:'first',title:'First',statusCode:'needs-review',lastModified:'100'}); first.className='recording-card';
  const second=make('article',{id:'second',title:'Second',statusCode:'ready',lastModified:'200'}); second.className='recording-card';
  library.children=[first,second];
  const control=()=>({parentElement:{},events:{},addEventListener(name,fn){this.events[name]=fn;}});
  const controls={'recording-filter':control(),'recording-sort':control(),'recording-pin':control(),'recording-group':control()};
  const progress={textContent:'',hidden:false};
  const stored=new Map([[storageKey,JSON.stringify({filter:'ready',sort:'alphabetical',pin:true,group:true})]]);
  const storage={getItem:k=>stored.get(k),setItem:(k,v)=>stored.set(k,v)};
  const document={querySelector:()=>({dataset:{phase:'ready',validated:'true',generation:'0'}}),getElementById:id=>({'scan-progress':progress,'recording-library':library,...controls})[id]||null,createElement:make};
  const fetch=async()=>({ok:true,json:async()=>({phase:'ready',validated:true,generation:1,recordings:[
    {id:'third',title:'A third',statusCode:'ready',status:'Ready',lastModified:'300',tracks:1,action:'View preview'},
    {id:'fourth',title:'Fourth',statusCode:'completed',status:'Completed',lastModified:'400',tracks:1,action:'View preview'}]})});
  runInNewContext(code,{document,window:{setTimeout:(fn,delay)=>timers.push({fn,delay}),localStorage:storage},fetch,URLSearchParams});
  assert.deepEqual(library.children,[second]);
  assert.equal(controls['recording-pin'].parentElement.hidden,true);
  assert.equal(controls['recording-group'].parentElement.hidden,true);
  await timers.shift().fn();
  assert.equal(library.children.length,1);
  assert.equal(library.children[0].dataset.id,'third');
  controls['recording-filter'].value='all'; controls['recording-filter'].events.change();
  assert.equal(library.children[0].className,'library-group-heading');
  assert.equal(library.children[1].dataset.id,'third');
  assert.equal(library.children[3].dataset.id,'fourth');
  assert.equal(controls['recording-pin'].disabled,true);
  controls['recording-group'].checked=false; controls['recording-group'].events.change();
  assert.equal(controls['recording-pin'].disabled,false);
  assert.equal(library.children.length,2);
  assert.equal(JSON.parse(stored.get(storageKey)).group,false);
  controls['recording-filter'].value='needs-review'; controls['recording-filter'].events.change();
  assert.match(library.children[0].children[0].textContent,/No recordings match this filter/);
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

test('library poll clears removed recordings and renders new cards', async () => {
  const timers = [];
  const make = tag => ({tag, className:'', dataset:{}, children:[], append(...nodes){this.children.push(...nodes);}, replaceChildren(...nodes){this.children=nodes;}});
  const library = make('div');
  const progress = {textContent:'', hidden:false};
  const controls = Object.fromEntries(['recording-filter','recording-sort','recording-pin','recording-group'].map(id => [id,{parentElement:{},addEventListener(){}}]));
  const document = {
    querySelector: () => ({dataset:{phase:'validating',validated:'false',generation:'0'}}),
    getElementById: id => ({'scan-progress':progress,'recording-library':library,...controls})[id] || null,
    createElement: make,
  };
  let calls=0;
  const fetch = async () => ({ok:true,json:async () => ++calls===1
    ? {phase:'validating',validated:false,generation:1,recordings:[],scanned:0,total:1}
    : {phase:'ready',validated:true,generation:2,recordings:[{id:'id & one',title:'Album <safe>',status:'Ready',action:'View preview',date:'1970',tracks:1,issues:['Check title']}],scanned:1,total:1}});
  runInNewContext(code,{document,window:{setTimeout:(fn,delay)=>timers.push({fn,delay}),localStorage:null},fetch,URLSearchParams});
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
