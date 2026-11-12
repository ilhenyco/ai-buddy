import test from 'node:test';
import assert from 'node:assert/strict';
import {makePlan,shiftPlanStart,seedSources,demoAnswer,submitAccess,EMPTY_ACCESS,dueBlocks,tasksForRole} from '../src/domain.ts';

test('a double confirmation keeps the same operation and confirmed reason',()=>{
  const first=submitAccess({...EMPTY_ACCESS},'Первая задача');
  const second=submitAccess(first,'Изменённое обоснование');
  assert.equal(second.operationId,first.operationId);
  assert.equal(second.number,first.number);
  assert.equal(second.reason,'Первая задача');
});
test('unknown service result retains the same operation on retry',()=>{
  const first={...submitAccess({...EMPTY_ACCESS},'Первая задача'),status:'unknown' as const};
  assert.equal(submitAccess(first,'Повтор').operationId,first.operationId);
  assert.equal(submitAccess(first,'Повтор').status,'unknown');
});
test('a foreign team source with a matching topic is never used',()=>{
  const foreign={...seedSources[0],team:'other',body:'Чужие закрытые сведения'};
  const answer=demoAnswer('Как получить доступы?',[foreign],'normal');
  assert.equal(answer.source,undefined);
  assert.match(answer.text,/не нашёл подтверждённого ответа/);
  assert.doesNotMatch(answer.text,/Чужие закрытые сведения/);
});
test('excluded and indexing sources cannot support a new answer',()=>{
  for(const status of ['excluded','indexing'] as const){
    const answer=demoAnswer('Как получить доступы?',[{...seedSources[0],status}],'normal');
    assert.equal(answer.source,undefined);
  }
});
test('model failure supplies a useful fallback without a fabricated source',()=>{
  const answer=demoAnswer('Как получить доступы?',seedSources,'model-error');
  assert.equal(answer.error,true);assert.equal(answer.source,undefined);assert.match(answer.text,/бадди/);
});
test('templates keep all lifecycle periods and publish concrete dates',()=>{
  const six=makePlan(6),nine=makePlan(9);
  assert.equal(six.blocks.length,8);assert.equal(nine.blocks.length,8);
  assert.equal(six.blocks[0].date,six.start);
  assert.notEqual(six.blocks.find(b=>b.id==='midpoint')!.date,nine.blocks.find(b=>b.id==='midpoint')!.date);
  assert.ok(six.blocks.every(b=>/^\d{4}-\d{2}-\d{2}$/.test(b.date)));
});
test('moving the start shifts dates while preserving edited content',()=>{
  const original=makePlan();original.blocks[1].intern='Отредактированное сообщение';
  const moved=shiftPlanStart(original,'2026-11-18');
  assert.equal(moved.blocks[1].date,'2026-11-25');
  assert.equal(moved.blocks[1].intern,'Отредактированное сообщение');
  assert.equal(original.blocks[1].date,'2026-11-18');
});
test('a newly supplied allowed source can support a demonstration answer',()=>{
  const answer=demoAnswer('Где проходит командная ретроспектива?',[...seedSources,{...seedSources[0],id:'new',title:'Командная ретроспектива',body:'Ретроспектива проходит по пятницам.'}],'normal');
  assert.equal(answer.source,'new');assert.match(answer.text,/пятницам/);
});

test('the bot delivers due checkpoints without requiring checklist completion',()=>{
  const plan=makePlan();
  assert.deepEqual(dueBlocks(plan,'2026-11-10',[]),[]);
  assert.deepEqual(dueBlocks(plan,'2026-11-11',[]).map(b=>b.id),['start']);
  assert.deepEqual(dueBlocks(plan,'2026-11-18',['start']).map(b=>b.id),['week']);
  assert.deepEqual(dueBlocks(plan,'2026-11-18',['start','week']),[]);
});

test('buddy and lead receive actionable copies linked to the same intern item',()=>{
  const week=makePlan().blocks.find(b=>b.id==='week')!;
  const buddy=tasksForRole(week,'buddy');const lead=tasksForRole(week,'lead');
  assert.equal(buddy.find(t=>t.id==='access')?.step,2);
  assert.equal(lead.find(t=>t.id==='access')?.step,2);
  assert.match(buddy.find(t=>t.id==='access')!.title,/Вместе со стажёром/);
  assert.match(lead.find(t=>t.id==='first-task')!.title,/Обсуди со стажёром/);
  assert.equal(lead.some(t=>t.id==='tools'),false);
});

test('a leader can explicitly remove a helper role from an individual item',()=>{
  const week=makePlan().blocks.find(b=>b.id==='week')!;
  week.tasks=week.tasks.map(t=>t.id==='access'?{...t,actions:{...t.actions,buddy:''}}:t);
  assert.equal(tasksForRole(week,'buddy').some(t=>t.id==='access'),false);
  assert.equal(tasksForRole(week,'intern').some(t=>t.id==='access'),true);
  assert.equal(tasksForRole(week,'lead').some(t=>t.id==='access'),true);
});
