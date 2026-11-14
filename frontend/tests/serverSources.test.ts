import test from 'node:test';
import assert from 'node:assert/strict';
import type { ApiSource } from '../src/api.ts';
import { anyIndexing, sourceFromServer, sourceIdFromLink } from '../src/serverSources.ts';

const s: ApiSource = { id: 'src-1', versionId: 'sv-1', title: 'Пропуск', body: 'Текст', category: 'Офис', acl: { teamIds: ['platform'] }, status: 'ready', updatedAt: '2026-11-14T18:30:00Z', etag: '2' };

test('server sources map to the materials page', () => {
  assert.deepEqual(sourceFromServer(s), { id: 'src-1', title: 'Пропуск', body: 'Текст', category: 'Офис', team: 'platform', status: 'ready', updated: '14 ноября в 21:30' });
  assert.equal(anyIndexing([s]), false);
  assert.equal(anyIndexing([s, { ...s, status: 'indexing' }]), true);
});

test('links of indexed materials open inside the app', () => {
  assert.equal(sourceIdFromLink('https://homych.invalid/sources/src-1'), 'src-1');
  assert.equal(sourceIdFromLink('https://github.com/avito-tech/playbook/blob/x/a.md'), null);
  assert.equal(sourceIdFromLink(undefined), null);
  assert.equal(sourceIdFromLink('https://homych.invalid/sources/'), null);
});
