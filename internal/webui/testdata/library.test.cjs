const {test}=require('node:test');
const assert=require('node:assert/strict');
const {readFileSync}=require('node:fs');
const {runInNewContext}=require('node:vm');
const {ordered}=require('../assets/status.js');
const {pageItems}=require('../assets/library.js');
test('search, status groups, dates, direction and stable ids compose before paging',()=>{
 const items=[{id:'b',title:'same',artist:'Band',relativePath:'tour/show',date:'',statusCode:'ready',tracks:2},{id:'a',title:'same',artist:'Band',relativePath:'tour/show',date:'1970',statusCode:'ready',tracks:2},{id:'c',title:'Else',artist:'Other',date:'1969',statusCode:'ready',playbackOnly:true}];
 for(const direction of ['asc','desc']) assert.equal(ordered([{id:'a',title:'a',lastModified:'0'},{id:'b',title:'b',lastModified:'123'}],{filter:'all',sort:'modified',direction})[1].id,'a');
 for(const direction of ['asc','desc'])assert.equal(ordered(items,{filter:'all',sort:'date',direction,pin:false})[2].id,'b');
 assert.deepEqual(ordered(items,{filter:'playback-only',sort:'artist'}).map(x=>x.id),['c']);
 assert.deepEqual(ordered(items,{filter:'all',sort:'tracks',direction:'desc',search:'TOUR'}).map(x=>x.id),['a','b']);
 const many=Array.from({length:5000},(_,i)=>({id:String(i),title:String(i),statusCode:'ready'}));
 assert.equal(pageItems(ordered(many,{filter:'all',sort:'alphabetical'}),50).length,100);
});
test('5000 recordings create at most 100 compact data rows and paging preserves focus',()=>{
 class Node{constructor(tag){this.tag=tag;this.dataset={};this.children=[];this.events={};this.parentElement={};this.scrollLeft=0;}append(...n){this.children.push(...n);}replaceChildren(...n){this.children=n;}setAttribute(){}addEventListener(k,f){this.events[k]=f;}querySelectorAll(){return this.children.flatMap(n=>[...(n.dataset.focus?[n]:[]),...n.querySelectorAll()]);}get lastElementChild(){return this.children.at(-1);}focus(){document.activeElement=this;}}
 const nodes=Object.fromEntries(['library-data','recording-library','library-pagination',...['filter','sort','pin','group','search','direction'].map(k=>'recording-'+k)].map(id=>[id,new Node('div')]));
 nodes['library-data'].textContent=JSON.stringify(Array.from({length:5000},(_,i)=>({id:String(i),title:'Title '+i,status:'Ready',statusCode:'ready',tracks:1,lastModified:'0',issues:[]})));
 let ready;const document={documentElement:{dataset:{layout:'compact'}},getElementById:id=>nodes[id],createElement:t=>new Node(t),addEventListener:(_,f)=>ready=f};
 const window={TrackTaggerLibrary:require('../assets/status.js'),addEventListener(){},scrollTo(){},scrollY:0};
 runInNewContext(readFileSync(require.resolve('../assets/library.js'),'utf8'),{document,window,URLSearchParams});ready();
 const lib=nodes['recording-library'];assert.equal(lib.children[0].children[1].children.length,100);
 const sort=lib.children[0].children[0].children[0].children[0].children[0];document.activeElement=sort;sort.events.click();assert.equal(document.activeElement.dataset.focus,'sort-status');
 lib.scrollLeft=17;lib.updateCards(JSON.parse(nodes['library-data'].textContent));assert.equal(lib.scrollLeft,17);assert.equal(document.activeElement.dataset.focus,'sort-status');
 nodes['library-pagination'].children[2].events.click();assert.equal(lib.children[0].children[1].children.length,100);
});

test('layout preference is applied synchronously and storage failures stay usable',()=>{
 const code=readFileSync(require.resolve('../assets/layout.js'),'utf8');
 for(const value of ['classic','invalid',null]){let ready;const doc={documentElement:{dataset:{}},addEventListener:(_,f)=>ready=f,getElementById:()=>null};runInNewContext(code,{document:doc,localStorage:{getItem:()=>value}});assert.equal(doc.documentElement.dataset.layout,value==='classic'?'classic':'compact');ready();}
 let ready,change;const feedback={},choice={addEventListener:(_,f)=>change=f};const doc={documentElement:{dataset:{}},addEventListener:(_,f)=>ready=f,getElementById:id=>id==='layout-choice'?choice:feedback};
 runInNewContext(code,{document:doc,localStorage:{getItem(){throw Error('blocked');},setItem(){throw Error('blocked');}}});assert.equal(doc.documentElement.dataset.layout,'compact');ready();choice.value='classic';change();assert.equal(doc.documentElement.dataset.layout,'classic');assert.match(feedback.textContent,/storage is unavailable/);
});
