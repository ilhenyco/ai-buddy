import test from 'node:test';
import assert from 'node:assert/strict';
import {makePlan} from '../src/domain.ts';
import {initialState,addScenario,patchScenario,publishScenario,deliverDue,migrateLegacy,restoreState,persistState,latestState,STORE,LEGACY_STORE,listScenarios,hasDraft,eventId,checkId,updateRuntime} from '../src/scenarios.ts';

const now='2026-11-12T10:00:00.000Z';
function variant(state=initialState(now),id='alternative',name='Саша Соколов'){
  return patchScenario(addScenario(state,id,'2026-11-11',now),id,{name:'Вариант '+id,intern:name},now);
}
test('separate incomplete drafts survive serialization and can be resumed independently',()=>{
  let state=variant();state=addScenario(state,'second','2026-11-11',now);
  state=patchScenario(state,'second',{name:'Другой вариант',buddy:'',start:''},now);
  let saved='';assert.equal(persistState({setItem:(_k,v)=>{saved=v;}},state),'');
  const result=restoreState({getItem:k=>k===STORE?saved:null});assert.equal(result.error,'');
  assert.equal(result.state.scenarios.find(s=>s.id==='alternative')?.draft.name,'Вариант alternative');
  assert.equal(result.state.scenarios.find(s=>s.id==='second')?.draft.buddy,'');
  assert.equal(result.state.scenarios.find(s=>s.id==='second')?.draft.start,'');
});
test('search and overlapping draft/published filters reflect revisions',()=>{
  let state=variant();state=patchScenario(state,'scenario-original',{name:'Правки первого'},now);
  assert.equal(listScenarios(state,'саша','all').length,2);
  assert.equal(listScenarios(state,'','drafts').length,2);
  assert.equal(listScenarios(state,'','published').length,1);
  assert.equal(listScenarios(state,'ПЕРВОГО','all')[0].id,'scenario-original');
  assert.equal(listScenarios(state,'нет совпадений','all').length,0);
});
test('draft edits do not change active published messages or start delivery for another intern',()=>{
  let state=variant(initialState(now),'masha','Маша Петрова');
  const original=state.scenarios[0].published!;
  state=patchScenario(state,'scenario-original',{blocks:original.blocks.map(b=>({...b,intern:'Правка черновика'}))},now);
  state=deliverDue(state,'2026-11-11');
  assert.notEqual(Object.values(state.runtimes['intern-sasha'].snapshots)[0].block.intern,'Правка черновика');
  assert.equal(state.runtimes['intern-masha'],undefined);
});
test('publishing only one variant per intern preserves the old variant and independent other interns',()=>{
  let state=variant();state=variant(state,'masha','Маша Петрова');
  state=publishScenario(state,'masha','2026-11-12',now);
  state=publishScenario(state,'alternative','2026-11-12',now);
  assert.equal(state.activeScenarios['intern-sasha'].scenarioId,'alternative');
  assert.equal(state.activeScenarios['intern-masha'].scenarioId,'masha');
  assert.ok(state.scenarios.find(s=>s.id==='scenario-original')?.published);
  assert.equal(hasDraft(state.scenarios.find(s=>s.id==='alternative')!),false);
});
test('replacement keeps past snapshots/checks/chat and does not backfill elapsed checkpoints',()=>{
  let state=deliverDue(initialState(now),'2026-11-18');
  state=updateRuntime(state,'intern-sasha',r=>({...r,checks:{[checkId('intern',eventId('scenario-original','start'),'hello')]:true},feed:[...r.feed,{kind:'chat',message:{id:'private',role:'user',text:'Личный вопрос',time:'10:00'}}]}));
  const before=structuredClone(state.runtimes['intern-sasha']);
  state=publishScenario(variant(state),'alternative','2026-11-18',now);
  state=deliverDue(state,'2026-11-18');
  assert.deepEqual(state.runtimes['intern-sasha'],before);
  state=deliverDue(state,'2026-11-25');
  assert.equal(state.runtimes['intern-sasha'].feed.length,before.feed.length+1);
  assert.equal(Object.values(state.runtimes['intern-sasha'].snapshots).at(-1)?.scenarioId,'alternative');
  assert.ok(state.runtimes['intern-sasha'].checks[checkId('intern',eventId('scenario-original','start'),'hello')]);
  assert.equal(deliverDue(state,'2026-11-25'),state);
});
test('republication of the same variant keeps sent snapshots unchanged',()=>{
  let state=deliverDue(initialState(now),'2026-11-11');
  const snapshot=structuredClone(state.runtimes['intern-sasha'].snapshots);
  state=patchScenario(state,'scenario-original',{blocks:state.scenarios[0].draft.blocks.map(b=>({...b,intern:'Новая редакция'}))},now);
  state=deliverDue(publishScenario(state,'scenario-original','2026-11-12',now),'2026-11-12');
  assert.deepEqual(state.runtimes['intern-sasha'].snapshots,snapshot);
  assert.equal(state.scenarios[0].published?.version,2);
});
test('a new intern receives first publication, with isolated history and checkbox keys',()=>{
  let state=publishScenario(variant(initialState(now),'masha','Маша Петрова'),'masha','2026-11-12',now);
  state=deliverDue(state,'2026-11-12');
  state=updateRuntime(state,'intern-masha',r=>({...r,checks:{[checkId('intern','masha:start','hello')]:true},feed:[...r.feed,{kind:'chat',message:{id:'secret',role:'user',text:'Только Маше',time:'10:00'}}]}));
  assert.equal(state.runtimes['intern-sasha'].feed.some(e=>e.kind==='chat'),false);
  assert.equal(Object.values(state.runtimes['intern-masha'].snapshots)[0].plan.intern,'Маша Петрова');
  assert.deepEqual(state.runtimes['intern-sasha'].checks,{});
});
test('published ownership cannot be moved to another intern',()=>{
  const state=patchScenario(initialState(now),'scenario-original',{intern:'Маша Петрова'},now);
  assert.equal(state.scenarios[0].internId,'intern-sasha');
  assert.equal(state.scenarios[0].draft.intern,'Саша Соколов');
});
test('legacy migration keeps draft, sources, dates, chats, snapshots and checked items together',()=>{
  const plan=makePlan();const draft=structuredClone(plan);draft.buddy='Игорь Волков';draft.months=9;
  const old={plan,draft,checks:{'intern:start:hello':true},feed:[{kind:'checkpoint',id:'start'},{kind:'chat',message:{id:'x',role:'user',text:'История',time:'10:00'}}],snapshots:{start:{block:plan.blocks[0],plan,next:plan.blocks[1]}},sources:[],demoDate:'2026-11-12'};
  const state=migrateLegacy(old,now),runtime=state.runtimes['intern-sasha'];
  assert.deepEqual(state.scenarios[0].draft,draft);assert.deepEqual(state.scenarios[0].published,plan);
  assert.equal(runtime.checks['intern:scenario-original:start:hello'],true);
  assert.equal(runtime.feed[0].kind==='checkpoint'&&runtime.feed[0].id,'scenario-original:start');
  assert.equal(runtime.feed[1].kind==='chat'&&runtime.feed[1].message.text,'История');
  assert.deepEqual(runtime.snapshots['scenario-original:start'].block,plan.blocks[0]);
  assert.equal(state.demoDate,old.demoDate);assert.deepEqual(state.sources,[]);
  assert.equal(deliverDue(state,'2026-11-12'),state);
  const legacyJSON=JSON.stringify(old);const values=new Map([[LEGACY_STORE,legacyJSON]]);
  const restored=restoreState({getItem:k=>values.get(k)??null});
  persistState({setItem:(k,v)=>{values.set(k,v);}},restored.state);
  assert.equal(values.get(LEGACY_STORE),legacyJSON);assert.ok(values.has(STORE));
});
test('save failure is reported and retry persists the same in-memory changes',()=>{
  const state=variant();let value='';
  assert.match(persistState({setItem:()=>{throw new Error('QuotaExceededError');}},state),/Не удалось сохранить/);
  assert.equal(persistState({setItem:(_k,v)=>{value=v;}},state),'');
  assert.equal(JSON.parse(value).scenarios.length,2);
});
test('incomplete scenarios are retained but cannot be published',()=>{
  const state=addScenario(initialState(now),'incomplete','2026-11-12',now);
  assert.throws(()=>publishScenario(state,'incomplete','2026-11-12',now),/Выбери стажёра/);
  assert.equal(state.activeScenarios['intern-sasha'].scenarioId,'scenario-original');
});
test('unreadable v3 data reports recovery error without modifying storage',()=>{
  const result=restoreState({getItem:()=>'{broken'});
  assert.match(result.error,/Автосохранение приостановлено/);
});
test('a stale tab reads latest data before editing without erasing another draft',()=>{
  const stale=initialState(now);let stored=JSON.stringify(stale);
  const storage={getItem:(k:string)=>k===STORE?stored:null,setItem:(_k:string,v:string)=>{stored=v;}};
  const first=variant(latestState(storage,stale),'masha','Маша Петрова');persistState(storage,first);
  const second=variant(latestState(storage,stale),'ilya','Илья Смирнов');persistState(storage,second);
  const result=restoreState(storage).state;
  assert.equal(result.scenarios.length,3);
  assert.equal(result.scenarios.find(s=>s.id==='masha')?.draft.intern,'Маша Петрова');
  assert.equal(result.scenarios.find(s=>s.id==='ilya')?.draft.intern,'Илья Смирнов');
  // A no-op must retain object identity so the scheduler cannot trigger render loops.
  assert.equal(latestState(storage,result),result);
});
