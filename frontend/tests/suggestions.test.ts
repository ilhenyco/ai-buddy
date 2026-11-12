import test from 'node:test';
import assert from 'node:assert/strict';
import {makePlan,shiftPlanStart,demoAnswer,seedSources} from '../src/domain.ts';
import {initialState,patchScenario,addScenario,publishScenario,updateRuntime,restoreState,persistState,STORE} from '../src/scenarios.ts';
import {questionPools,blockTopic,suggestionStage,pickQuestions,ensureSuggestions,type SuggestionTopic} from '../src/suggestions.ts';

const noDraw=()=>{throw new Error('The selection must remain stable');};
test('every stage supplies three unique product questions with a random draw',()=>{
  for(const topic of Object.keys(questionPools) as SuggestionTopic[]){
    for(const rng of [()=>0,()=>0.4,()=>0.999]){
      const questions=pickQuestions(topic,rng);
      assert.equal(questions.length,3);assert.equal(new Set(questions).size,3);
      assert.ok(questions.every(q=>questionPools[topic].includes(q)));
    }
    assert.notDeepEqual(pickQuestions(topic,()=>0),pickQuestions(topic,()=>0.999));
  }
});
test('the stage follows actual published dates for both durations and shifted starts',()=>{
  for(const months of [6,9] as const){
    const plan=shiftPlanStart(makePlan(months),'2027-02-01');
    for(const block of plan.blocks)assert.deepEqual(suggestionStage(plan,block.date),{stageKey:'block:'+block.id,topic:block.stageKind});
    const reverse={...plan,blocks:[...plan.blocks].reverse()};
    assert.deepEqual(suggestionStage(reverse,plan.blocks[4].date),suggestionStage(plan,plan.blocks[4].date));
  }
  const six=makePlan(6),nine=makePlan(9);
  assert.equal(suggestionStage(six,'2027-02-12').topic,'midpoint');
  assert.equal(suggestionStage(nine,'2027-02-12').topic,'practice');
});
test('before the first block startup questions change only when the first stage arrives',()=>{
  const before=ensureSuggestions(initialState(),'2026-11-10',()=>0);
  assert.equal(before.runtimes['intern-sasha'].suggestions?.stageKey,'prestart');
  assert.equal(ensureSuggestions(before,'2026-11-10',noDraw),before);
  const started=ensureSuggestions(before,'2026-11-11',()=>0);
  assert.equal(started.runtimes['intern-sasha'].suggestions?.stageKey,'block:start');
  assert.notDeepEqual(new Set(started.runtimes['intern-sasha'].suggestions?.questions),new Set(before.runtimes['intern-sasha'].suggestions?.questions));
});
test('missing published plans never receive a suggestion selection',()=>{
  const state=initialState();state.activeScenarios={};
  const next=ensureSuggestions(state,'2027-05-01',noDraw);
  assert.equal(next,state);assert.equal(next.runtimes['intern-sasha'].suggestions,undefined);
});
test('answers, checkmarks, time within one stage and reload retain the exact selection',()=>{
  let state=ensureSuggestions(initialState(),'2026-11-11',()=>0);
  const saved=structuredClone(state.runtimes['intern-sasha'].suggestions);
  state=updateRuntime(state,'intern-sasha',r=>({...r,checks:{checked:true},feed:[{kind:'chat',message:{id:'q',role:'user',text:'Произвольный вопрос',time:'09:00'}},{kind:'chat',message:{id:'a',role:'assistant',text:'Ответ',time:'09:00'}}]}));
  let stored='';persistState({setItem:(_k,v)=>{stored=v;}},state);
  const restored=restoreState({getItem:k=>k===STORE?stored:null}).state;
  assert.equal(ensureSuggestions(restored,'2026-11-17',noDraw),restored);
  assert.deepEqual(restored.runtimes['intern-sasha'].suggestions,saved);
});
test('draft changes do not move the published stage or redraw its questions',()=>{
  let state=ensureSuggestions(initialState(),'2026-11-18',()=>0);
  const selection=state.runtimes['intern-sasha'].suggestions;
  state=patchScenario(state,'scenario-original',shiftPlanStart(state.scenarios[0].draft,'2027-05-01'));
  assert.equal(ensureSuggestions(state,'2026-11-18',noDraw),state);
  assert.equal(state.runtimes['intern-sasha'].suggestions,selection);
});
test('republishing or replacing a variant at the same stage retains its selection',()=>{
  let state=ensureSuggestions(initialState(),'2026-11-18',()=>0);
  const selection=structuredClone(state.runtimes['intern-sasha'].suggestions);
  state=patchScenario(state,'scenario-original',{name:'Другое название'});
  state=publishScenario(state,'scenario-original','2026-11-18');
  assert.equal(ensureSuggestions(state,'2026-11-18',noDraw),state);
  state=addScenario(state,'alternative','2026-11-11');
  state=patchScenario(state,'alternative',{intern:'Саша Соколов'});
  state=publishScenario(state,'alternative','2026-11-18');
  assert.equal(ensureSuggestions(state,'2026-11-18',noDraw),state);
  assert.deepEqual(state.runtimes['intern-sasha'].suggestions,selection);
});
test('each intern keeps an independent selection and another intern changing stage cannot redraw it',()=>{
  let state=addScenario(initialState(),'masha','2026-11-18');
  state=patchScenario(state,'masha',{intern:'Маша Петрова'});
  state=publishScenario(state,'masha','2026-11-18');
  state=ensureSuggestions(state,'2026-11-18',()=>0);
  assert.equal(state.runtimes['intern-sasha'].suggestions?.topic,'week');
  assert.equal(state.runtimes['intern-masha'].suggestions?.topic,'start');
  const masha=state.runtimes['intern-masha'].suggestions;
  const next=ensureSuggestions(state,'2026-11-25',()=>0.9);
  assert.equal(next.runtimes['intern-masha'].suggestions?.topic,'week');
  assert.equal(masha?.topic,'start');
  const sameStage=ensureSuggestions(next,'2026-11-26',()=>0);
  assert.equal(sameStage.runtimes['intern-masha'].suggestions,next.runtimes['intern-masha'].suggestions);
});
test('library copies and legacy IDs inherit topics, including the final-prep prefix',()=>{
  const block=makePlan().blocks.find(b=>b.id==='final-prep')!;
  assert.equal(blockTopic({...block,id:'new-copy-id'}),'final-prep');
  assert.equal(blockTopic({...block,id:'final-prep-old-copy',stageKind:undefined}),'final-prep');
  assert.equal(blockTopic({...block,id:'week-old-copy',stageKind:undefined}),'week');
  assert.equal(blockTopic({...block,id:'unknown-custom',stageKind:undefined}),'general');
});
test('a later copy of the same topic is a new stage with a visibly different set',()=>{
  let state=initialState();const plan=state.scenarios[0].published!;
  const start=plan.blocks[0];
  state.scenarios[0].published={...plan,blocks:[start,{...start,id:'start-copy',date:'2026-11-12'}]};
  state=ensureSuggestions(state,'2026-11-11',()=>0);
  const previous=new Set(state.runtimes['intern-sasha'].suggestions?.questions);
  state=ensureSuggestions(state,'2026-11-12',()=>0);
  assert.equal(state.runtimes['intern-sasha'].suggestions?.stageKey,'block:start-copy');
  assert.notDeepEqual(new Set(state.runtimes['intern-sasha'].suggestions?.questions),previous);
});
test('old v3 without selections is upgraded without changing its history, checks or plans',()=>{
  const old=initialState();old.runtimes['intern-sasha'].checks.keep=true;
  old.runtimes['intern-sasha'].feed.push({kind:'chat',message:{id:'old',role:'user',text:'Личная история',time:'09:00'}});
  for(const b of old.scenarios[0].published!.blocks)delete b.stageKind;
  const restored=restoreState({getItem:k=>k===STORE?JSON.stringify(old):null});
  assert.equal(restored.error,'');
  const next=ensureSuggestions(restored.state,'2026-11-25',()=>0);
  assert.equal(next.schema,3);assert.equal(next.runtimes['intern-sasha'].suggestions?.topic,'adaptation');
  assert.deepEqual(next.scenarios,old.scenarios);assert.deepEqual(next.runtimes['intern-sasha'].feed,old.runtimes['intern-sasha'].feed);
  assert.deepEqual(next.runtimes['intern-sasha'].checks,old.runtimes['intern-sasha'].checks);
});
test('invalid or repeated persisted questions are repaired without touching the conversation',()=>{
  for(const questions of [['Кто мой бадди?','Кто мой бадди?','Посторонний вопрос'],{},null,'not-an-array']){
    const state=initialState();state.runtimes['intern-sasha'].suggestions=JSON.parse(JSON.stringify({stageKey:'block:start',topic:'start',questions}));
    const repaired=ensureSuggestions(state,'2026-11-11',()=>0);
    assert.equal(new Set(repaired.runtimes['intern-sasha'].suggestions?.questions).size,3);
    assert.deepEqual(repaired.runtimes['intern-sasha'].feed,state.runtimes['intern-sasha'].feed);
  }
});
test('leave questions without an allowed instruction cannot fabricate policy or a citation',()=>{
  for(const query of ['Как взять отгул?','Как взять отпуск?']){
    const reply=demoAnswer(query,seedSources,'normal');
    assert.equal(reply.source,undefined);assert.match(reply.text,/не нашёл подтверждённого ответа/);assert.match(reply.text,/HR/);
  }
  const source={...seedSources[0],id:'leave',title:'Как взять отпуск?',body:'Демонстрация: порядок согласования уточняет HR.'};
  assert.equal(demoAnswer('Как взять отпуск?',[source],'normal').source,'leave');
  for(const invalid of [{...source,team:'other'},{...source,status:'excluded' as const},{...source,status:'indexing' as const}])assert.equal(demoAnswer('Как взять отпуск?',[invalid],'normal').source,undefined);
});
