import test from 'node:test';
import assert from 'node:assert/strict';
import { setFetch, type AccessOperation } from '../src/api.ts';
import { ACCESS_RESOURCE, accessFromServer, accessMode, accessStatusText, sameAccess, submitAccessRequest } from '../src/serverAccess.ts';

const op = (patch: Partial<AccessOperation> = {}): AccessOperation => ({
  id: 'op-1', assignmentId: 'assignment-sasha', resourceId: ACCESS_RESOURCE.id, correlationId: 'corr-1',
  externalNumber: 'MOCK-1042', status: 'pending', submissionConfirmed: true, updatedAt: '2026-11-12T09:00:00Z', ...patch,
});

test('operations map to the screen state; other resources are ignored', () => {
  assert.deepEqual(accessFromServer([], 'r'), { operationId: null, number: null, status: 'none', resource: ACCESS_RESOURCE.title, reason: 'r' });
  const a = accessFromServer([op({ resourceId: 'gitlab-team', id: 'op-0' }), op({ status: 'approved' })], 'r');
  assert.equal(a.operationId, 'op-1');
  assert.equal(a.status, 'approved');
  assert.equal(a.number, 'MOCK-1042');
  assert.match(accessStatusText(a), /одобрен · MOCK-1042/);
  assert.match(accessStatusText(accessFromServer([op({ status: 'unknown', externalNumber: null })])), /без повторной заявки/);
  assert.match(accessStatusText(accessFromServer([op()])), /^Заявка MOCK-1042 на согласовании/);
  assert.ok(sameAccess(a, { ...a, reason: 'other' }));
  assert.ok(!sameAccess(a, accessFromServer([op()])));
});

test('states panel options become mock modes', () => {
  assert.equal(accessMode('approved'), 'approve');
  assert.equal(accessMode('denied'), 'deny');
  assert.equal(accessMode('unknown'), 'lost');
});

test('a request is a draft plus an explicit confirmation with stable idempotency keys', async () => {
  const calls: { url: string; body: any; key: string | null }[] = [];
  setFetch(async (url, init) => {
    const body = init?.body ? JSON.parse(String(init.body)) : null;
    const key = new Headers(init?.headers).get('Idempotency-Key');
    calls.push({ url: String(url), body, key });
    const json = String(url).endsWith('/confirm') ? op() : { id: 'ad-1', version: 1, assignmentId: body.assignmentId, resourceId: body.resourceId, reason: body.reason, expiresAt: '' };
    return new Response(JSON.stringify(json), { status: 201, headers: { 'Content-Type': 'application/json' } });
  });
  const result = await submitAccessRequest('assignment-sasha', 'Для первой задачи', { draft: 'k1', confirm: 'k2' });
  assert.equal(result.externalNumber, 'MOCK-1042');
  assert.deepEqual(calls.map(c => [c.url, c.key]), [['/api/v1/access-drafts', 'k1'], ['/api/v1/access-drafts/ad-1/confirm', 'k2']]);
  assert.deepEqual(calls[0].body, { assignmentId: 'assignment-sasha', resourceId: 'repo-learning-read', reason: 'Для первой задачи' });
  assert.deepEqual(calls[1].body, { draftVersion: 1, reason: 'Для первой задачи', confirmed: true });
});
