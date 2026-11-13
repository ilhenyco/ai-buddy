import test from 'node:test';
import assert from 'node:assert/strict';
import { makePlan, tasksForRole } from '../src/domain.ts';
import { checkId, emptyRuntime, type Runtime } from '../src/scenarios.ts';
import { buildPlanPanel } from '../src/planPanel.ts';

const plan = makePlan(6);
const [b0, b1] = [...plan.blocks].sort((a, b) => a.date.localeCompare(b.date));
const runtime = (checks: Record<string, boolean> = {}): Runtime => ({
  ...emptyRuntime(), checks,
  snapshots: { 'cp-0': { scenarioId: 's', block: b0, plan }, 'cp-1': { scenarioId: 's', block: b1, plan } },
});

test('intern: delivered stages newest first, progress from the same checks as the chat', () => {
  const first = b0.tasks[0];
  const p = buildPlanPanel(runtime({ [checkId('intern', 'cp-0', first.id)]: true }), 'intern');
  assert.deepEqual(p.stages.map(s => s.checkpointId), ['cp-1', 'cp-0']);
  assert.equal(p.current?.checkpointId, 'cp-1');
  assert.ok(p.stages[0].current && !p.stages[1].current);
  assert.equal(p.stages[1].done, 1);
  assert.equal(p.stages[1].tasks.find(t => t.id === first.id)?.done, true);
  assert.equal(p.total, b0.tasks.length + b1.tasks.length);
  assert.equal(p.done, 1);
});

test('links of all stages are listed once, the first stage keeps them reachable', () => {
  const p = buildPlanPanel(runtime(), 'intern');
  const keys = p.links.map(l => l.key);
  assert.equal(new Set(keys).size, keys.length);
  for (const t of b0.tasks) assert.ok(p.links.some(l => l.label === t.link), 'link of the first day: ' + t.link);
  assert.equal(p.links[0].stage, b0.title);
});

test('upcoming stages: titles and dates only, after the last delivered stage', () => {
  const p = buildPlanPanel(runtime(), 'intern');
  assert.ok(p.upcoming.length > 0);
  assert.ok(p.upcoming.every(u => u.date > b1.date));
  assert.deepEqual(Object.keys(p.upcoming[0]).sort(), ['date', 'title']);
  // server mode knows only the delivered blocks: the "next" of the message is used
  const short = { ...plan, blocks: [b0] };
  const r: Runtime = { ...emptyRuntime(), snapshots: { 'cp-0': { scenarioId: 's', block: b0, plan: short, next: b1 } } };
  assert.deepEqual(buildPlanPanel(r, 'intern').upcoming, [{ title: b1.title, date: b1.date }]);
});

test('buddy sees only stages and tasks addressed to the buddy; empty runtime is empty', () => {
  const p = buildPlanPanel(runtime(), 'buddy');
  for (const s of p.stages) assert.equal(s.total, tasksForRole(s.checkpointId === 'cp-0' ? b0 : b1, 'buddy').length);
  assert.ok(p.stages.every(s => s.total > 0));
  const empty = buildPlanPanel(emptyRuntime(), 'intern');
  assert.equal(empty.current, null);
  assert.deepEqual([empty.stages, empty.links, empty.upcoming], [[], [], []]);
  assert.deepEqual(buildPlanPanel(runtime(), 'hr').stages, []);
});
