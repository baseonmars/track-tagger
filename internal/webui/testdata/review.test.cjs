const {test} = require('node:test');
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const {runInNewContext} = require('node:vm');
const {actionState, validDate} = require('../assets/review.js');
const base = {reviewed:true,writable:true,status:'ready',changed:false,approved:false,valid:true,submitting:false};
test('saved review and approval govern actions', () => {
  assert.equal(actionState(base).apply,true);
  assert.equal(actionState(base).save,false);
  for (const state of [{reviewed:false},{changed:true},{valid:false},{writable:false},{status:'waiting'},{status:'completed'},{submitting:true}]) assert.equal(actionState({...base,...state}).apply,false,JSON.stringify(state));
  assert.equal(actionState({...base,approved:true,changed:true}).label,'Save and apply');
  assert.equal(actionState({...base,reviewed:false}).label,'Save and apply');
  assert.equal(actionState({...base,approved:true,status:'waiting'}).save,true);
  assert.equal(actionState({...base,approved:true,changed:true,status:'completed'}).apply,true);
  assert.equal(actionState({...base,approved:true,status:'completed'}).apply,false);
});
test('calendar dates match server supported precisions', () => {
 for (const value of ['1970','1970-01','2000-02-29','2024-10-24']) assert.equal(validDate(value),true,value);
 for (const value of ['','0000','1970-00','1970-13','1900-02-29','1970-02-30','1970-1','1970-01-01-extra']) assert.equal(validDate(value),false,value);
});
function setup(options={}) {
 const node = () => ({disabled:true,textContent:'',events:{},addEventListener(name,fn){this.events[name]=fn;}});
 const input = (name,value,type='text') => ({name,value,type,checked:false});
 const fields = [input('artist','Band'),input('album','Concert'),input('date','1970'),input('date_unknown','yes','checkbox'),input('track_file','t01.flac'),input('track_title','Song'),input('track_artist',''),input('track_disc','1'),input('track_number','1'),input('approve','yes','checkbox')];
 fields.namedItem = name => fields.find(f=>f.name===name);
 const form = Object.assign(node(),{elements:fields,dataset:{reviewed:'true',writable:'true',status:'ready',...options},checkValidity:()=>true});
 const nodes = {'review-form':form,'save-review':node(),'apply-review':node(),'review-help':node()};
 const window = node();
 runInNewContext(readFileSync(require('node:path').join(__dirname,'../assets/review.js'),'utf8'),{document:{getElementById:id=>nodes[id]},window});
 return {form,fields,nodes,window};
}
test('DOM edits, exact reverts, approval, and submitting protect both buttons', () => {
 const {form,fields,nodes,window}=setup();
 const apply=nodes['apply-review'], save=nodes['save-review'];
 assert.equal(apply.disabled,false); assert.equal(save.disabled,true);
 fields.namedItem('track_title').value='Edited'; form.events.input();
 assert.equal(apply.disabled,true); assert.equal(apply.textContent,'Save and apply');
 fields.namedItem('track_title').value='Song'; form.events.input();
 assert.equal(apply.disabled,false); assert.equal(apply.textContent,'Apply saved review');
 fields.namedItem('approve').checked=true; form.events.change();
 assert.equal(save.disabled,false); assert.equal(apply.textContent,'Save and apply');
 let prevented=false;
 form.events.submit({submitter:apply,preventDefault(){prevented=true;}});
 assert.equal(prevented,false); assert.equal(form.action,"/apply"); assert.equal(apply.disabled,true); assert.equal(save.disabled,true);
 form.events.submit({submitter:apply,preventDefault(){prevented=true;}});
 assert.equal(prevented,true);
 window.events.pageshow(); assert.equal(apply.disabled,false);
});
test('DOM invalid dates, blank titles, and approval block forged submits',()=>{
 const {form,fields,nodes}=setup({reviewed:'false'});
 assert.equal(nodes['apply-review'].disabled,true);
 let blocked=false;
 form.events.submit({submitter:nodes['save-review'],preventDefault(){blocked=true;}});
 assert.equal(blocked,true);
 fields.namedItem('approve').checked=true;
 for (const [name,value] of [['date','1970-02-30'],['track_title',' '],['track_disc','0']]) {
  const field=fields.namedItem(name), old=field.value; field.value=value; form.events.input();
  assert.equal(nodes['save-review'].disabled,true,name); field.value=old;
 }
 form.events.input(); assert.equal(nodes['save-review'].disabled,false);
});
