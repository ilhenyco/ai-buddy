import test from 'node:test';
import assert from 'node:assert/strict';
import { makePlan } from '../src/domain.ts';
import { checkId, initialState } from '../src/scenarios.ts';
import type { Assignment, FeedPage, SessionContext } from '../src/api.ts';
import { planToServer } from '../src/serverScenarios.ts';
import { applyServerFeed, assignmentFor, serverToday } from '../src/serverMessages.ts';

const p = (id: string, displayName: string) => ({ id, displayName });
const a: Assignment = { id: 'assignment-sasha', intern: p('u-sasha', 'Саша Соколов'), buddy: p('u-dasha', 'Даша Морозова'), lead: p('u-alex', 'Алексей Романов'), teamId: 'platform', capabilities: ['feed:read'] };
const ctx: SessionContext = { subject: p('u-sasha', 'Саша Соколов'), assignments: [a], capabilities: ['feed:read', 'questions:ask'], serverTime: '', csrfToken: '' };
const blocks = planToServer(makePlan(6), []).blocks;
const msg = (id: string, i: number, next = true) => ({
  id, assignmentId: a.id, kind: 'checkpoint' as const, recipientUserId: 'u-sasha', createdAt: '2026-11-12T06:00:00Z',
  text: blocks[i].intern, scenarioId: 'scn-demo-sasha', revisionId: 'rev-1', block: blocks[i],
  ...(next ? { next: { id: blocks[i + 1].id, title: blocks[i + 1].title, date: blocks[i + 1].date } } : {}),
});
const feed = (items: FeedPage['items'], checks: FeedPage['checks'] = []): FeedPage => ({
  items, checks, nextCursor: null, serverTime: '2026-11-12T09:00:00Z',
  activePlan: { scenarioId: 'scn-demo-sasha', revisionId: 'rev-1', start: '2026-11-11', months: 6 },
});

test('helpers: assignment of the intern on screen, Moscow date of the server', () => {
  assert.equal(assignmentFor(ctx, 'u-sasha')?.id, 'assignment-sasha');
  assert.equal(assignmentFor(ctx, 'u-kirill'), undefined);
  assert.equal(serverToday('2026-11-12T21:30:00Z'), '2026-11-13'); // 00:30 in Moscow
});

test('server messages become snapshots and feed entries; local AI chat stays in place', () => {
  const state = initialState();
  state.runtimes['u-sasha'] = {
    checks: { stale: true },
    feed: [{ kind: 'checkpoint', id: 'local-old' }, { kind: 'chat', message: { id: 'c1', role: 'user', text: 'Привет', time: '10:00' } as any }],
    snapshots: {},
  };
  let s = applyServerFeed(state, 'u-sasha', 'intern', a, feed([msg('m1', 0)]), '2026-11-12', () => 0);
  let r = s.runtimes['u-sasha'];
  assert.deepEqual(r.feed.map(e => e.kind === 'chat' ? 'chat' : e.id), ['chat', 'm1']);
  assert.equal(r.snapshots.m1.block.id, 'start');
  assert.equal(r.snapshots.m1.block.eyebrow, makePlan(6).blocks[0].eyebrow); // look from the template
  assert.equal(r.snapshots.m1.plan.buddy, 'Даша Морозова');
  assert.equal(r.snapshots.m1.next?.id, 'week');
  assert.deepEqual(r.checks, {});
  assert.equal(r.suggestions?.stageKey, 'block:start');

  // a new message is appended after the chat, checks come from the server
  s = applyServerFeed(s, 'u-sasha', 'intern', a, feed([msg('m1', 0), msg('m2', 1)], [{ messageId: 'm2', taskId: 'access', userId: 'u-sasha', checked: true, updatedAt: '' }]), '2026-11-18', () => 0);
  r = s.runtimes['u-sasha'];
  assert.deepEqual(r.feed.map(e => e.kind === 'chat' ? 'chat' : e.id), ['chat', 'm1', 'm2']);
  assert.equal(r.checks[checkId('intern', 'm2', 'access')], true);
  assert.equal(r.suggestions?.stageKey, 'block:week'); // new stage -> new questions

  // the same feed again changes nothing (no re-render)
  assert.equal(applyServerFeed(s, 'u-sasha', 'intern', a, feed([msg('m1', 0), msg('m2', 1)], [{ messageId: 'm2', taskId: 'access', userId: 'u-sasha', checked: true, updatedAt: '' }]), '2026-11-18'), s);
});

test('buddy gets no intern suggestions; last block has no "next"; no plan keeps suggestions as they are', () => {
  const s = applyServerFeed(initialState(), 'u-sasha', 'buddy', a, feed([msg('m9', 7, false)]), '2026-11-12');
  const r = s.runtimes['u-sasha'];
  assert.equal(r.suggestions, undefined);
  assert.equal(r.snapshots.m9.next, undefined);
  const before = initialState();
  assert.equal(applyServerFeed(before, 'u-sasha', 'intern', a, { ...feed([]), activePlan: null }, '2026-11-12'), before); // nothing to show yet
});
