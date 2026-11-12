import test from 'node:test';
import assert from 'node:assert/strict';
import { makePlan } from '../src/domain.ts';
import { initialState } from '../src/scenarios.ts';
import { api, ApiError, errorText, setFetch, type ApiScenario, type SessionContext } from '../src/api.ts';
import { applyServerScenarios, describeFields, internsOf, planFromServer, planToServer, roleOf } from '../src/serverScenarios.ts';

const p = (id: string, displayName: string) => ({ id, displayName });
const ctx: SessionContext = {
  subject: p('u-alex', 'Алексей Романов'),
  assignments: [
    { id: 'assignment-sasha', intern: p('u-sasha', 'Саша Соколов'), buddy: p('u-dasha', 'Даша Морозова'), lead: p('u-alex', 'Алексей Романов'), teamId: 'platform', capabilities: ['feed:read', 'scenarios:write'] },
    { id: 'assignment-masha', intern: p('u-masha', 'Маша Петрова'), buddy: p('u-dasha', 'Даша Морозова'), lead: p('u-alex', 'Алексей Романов'), teamId: 'platform', capabilities: ['feed:read', 'scenarios:write'] },
  ],
  capabilities: ['feed:read', 'scenarios:write'], serverTime: '2026-11-12T09:00:00Z', csrfToken: 'csrf-1',
};
const interns = internsOf(ctx);

test('roles come from server relations, not from the browser', () => {
  assert.equal(roleOf(ctx), 'lead');
  assert.equal(roleOf({ ...ctx, subject: p('u-dasha', 'Даша'), capabilities: ['feed:read'] }), 'buddy');
  assert.equal(roleOf({ ...ctx, subject: p('u-sasha', 'Саша'), capabilities: ['feed:read', 'questions:ask'] }), 'intern');
  assert.equal(roleOf({ ...ctx, subject: p('u-anna', 'Анна'), assignments: [], capabilities: [] }), 'hr');
  assert.deepEqual(interns.map(i => i.id), ['u-sasha', 'u-masha']);
});

test('local plan -> contract plan: ids instead of names, only contract fields', () => {
  const local = makePlan(6);
  local.blocks[0].tasks[0] = { ...local.blocks[0].tasks[0], url: '  https://avito.ru/x ', kind: 'intern' as any, actions: { buddy: 'Помоги', lead: '  ' } };
  const out = planToServer(local, interns);
  assert.equal(out.internId, 'u-sasha');
  assert.equal(out.buddyId, null); // bound by the server
  assert.equal(out.teamId, 'Backend · Платформа');
  const t = out.blocks[0].tasks[0];
  assert.equal(t.url, 'https://avito.ru/x');
  assert.equal(t.kind, 'team'); // the contract has no "intern" kind
  assert.deepEqual(t.actions, { buddy: 'Помоги' });
  assert.ok(!('eyebrow' in out.blocks[0]) && !('process' in out.blocks[0]));
  assert.ok(!('url' in out.blocks[1].tasks[0]) && !('actions' in out.blocks[3].tasks[0] && false));
  assert.equal(planToServer({ ...local, intern: '', team: '' }, interns).internId, null);
  assert.equal(planToServer({ ...local, intern: '', team: '' }, interns).teamId, 'default');
});

test('contract plan -> local plan: names from the directory, visual fields from the template', () => {
  const local = makePlan(9);
  const server = { ...planToServer(local, interns), buddyId: 'u-dasha', leadId: 'u-alex' };
  server.blocks.push({ ...server.blocks[1], id: 'week-0b1c', endDate: undefined });
  const back = planFromServer(server, new Map(interns.map(i => [i.id, i.name] as [string, string]).concat([['u-dasha', 'Даша Морозова'], ['u-alex', 'Алексей Романов']])), 3);
  assert.equal(back.intern, 'Саша Соколов');
  assert.equal(back.buddy, 'Даша Морозова');
  assert.equal(back.version, 3);
  assert.equal(back.blocks[1].eyebrow, local.blocks[1].eyebrow);
  assert.equal(back.blocks.at(-1)!.eyebrow, local.blocks[1].eyebrow); // copied block keeps its template look
  assert.equal(back.blocks.at(-1)!.endDate, '');
  assert.equal(planFromServer({ ...server, internId: 'u-unknown' }, new Map()).intern, '');
});

test('server list replaces local scenarios and active variants, keeps message history', () => {
  const plan = planToServer(makePlan(6), interns);
  const scenario = (id: string, internId: string | null, active: boolean, published: boolean): ApiScenario => ({
    id, assignmentId: internId ? 'a-' + internId : null, draft: { ...plan, internId }, draftVersion: 2,
    published: published ? { id: 'rev-' + id, number: 1, publishedAt: '2026-11-12T09:00:00Z', plan: { ...plan, internId } } : null,
    isActive: active, hasUnpublishedChanges: !published, updatedAt: '2026-11-12T09:00:00Z',
  });
  const before = initialState();
  before.runtimes['u-sasha'] = { checks: { x: true }, feed: [], snapshots: {} };
  const after = applyServerScenarios(before, ctx, [scenario('s1', 'u-sasha', true, true), scenario('s2', 'u-sasha', false, true), scenario('s3', null, false, false)], { s1: '2026-11-20' });
  assert.deepEqual(after.interns.map(i => i.id), ['u-sasha', 'u-masha']);
  assert.equal(after.scenarios.length, 3);
  assert.deepEqual(after.activeScenarios, { 'u-sasha': { scenarioId: 's1', afterDate: '2026-11-20' } });
  assert.equal(after.scenarios[0].published?.intern, 'Саша Соколов');
  assert.equal(after.scenarios[2].published, undefined);
  assert.equal(after.runtimes['u-sasha'].checks.x, true);
  assert.equal(applyServerScenarios(before, { ...ctx, assignments: [] }, []).interns, before.interns);
});

test('validation fields are described in words', () => {
  assert.equal(describeFields([]), '');
  assert.equal(describeFields(['name', 'blocks[1].title', 'blocks[0].tasks[2].url']), 'Проверьте: название; блок 2: название; блок 1, пункт 3: ссылка.');
  assert.match(describeFields(['a', 'b', 'c', 'd', 'e']), /и ещё 1/);
});

test('api client sends cookies, CSRF, If-Match and Idempotency-Key and maps errors', async () => {
  const calls: { url: string; init: RequestInit }[] = [];
  const reply = (status: number, body: unknown) => new Response(body === null ? null : JSON.stringify(body), { status });
  const queue: Response[] = [
    reply(200, { ...ctx }),
    reply(200, { id: 's1', draftVersion: 3 }),
    reply(412, { code: 'VERSION_CONFLICT', message: 'x', requestId: 'r1', details: { currentVersion: 5 } }),
    reply(409, { code: 'CONFLICT', message: 'x', requestId: 'r2', details: { reason: 'REPLACEMENT_NOT_CONFIRMED' } }),
    reply(204, null),
  ];
  setFetch(async (url, init) => { calls.push({ url: String(url), init: init ?? {} }); return queue.shift()!; });

  await api.login('u-alex');
  await api.saveDraft('s1', 2, planToServer(makePlan(6), interns));
  const h = calls[1].init.headers as Record<string, string>;
  assert.equal(calls[1].url, '/api/v1/scenarios/s1/draft');
  assert.equal(h['If-Match'], '"2"');
  assert.equal(h['X-CSRF-Token'], 'csrf-1');
  assert.equal(calls[1].init.credentials, 'same-origin');

  await assert.rejects(api.publish('s1', 2, null, false, 'key-1'), (e: unknown) => e instanceof ApiError && e.status === 412 && /другой вкладке/.test(errorText(e)));
  assert.equal((calls[2].init.headers as Record<string, string>)['Idempotency-Key'], 'key-1');
  await assert.rejects(api.createScenario({ assignmentId: null, months: 6, start: '2026-11-11', name: 'x' }), (e: unknown) => e instanceof ApiError && errorText(e).includes('Подтвердите замену'));
  await api.logout();

  setFetch(async () => { throw new TypeError('offline'); });
  await assert.rejects(api.listScenarios('маша', 'drafts'), (e: unknown) => e instanceof ApiError && e.status === 0);
  assert.equal(errorText(new Error('x')), 'Что-то пошло не так. Попробуйте ещё раз.');
  assert.equal(errorText(new ApiError(401, 'UNAUTHENTICATED', 'x')), 'Сессия закончилась. Войдите снова.');
  assert.equal(errorText(new ApiError(409, 'CONFLICT', 'x', { reason: 'PUBLISHED_INTERN_LOCKED' })), 'У опубликованного сценария нельзя сменить стажёра.');
  assert.equal(errorText(new ApiError(422, 'VALIDATION_ERROR', 'Проверьте поля', { fields: ['name'] })), 'Проверьте поля');
});
