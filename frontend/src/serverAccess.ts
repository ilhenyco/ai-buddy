// Access requests in server mode: maps the server's operations to the screen's Access state
// and sends a request as draft + explicit confirmation (two idempotent calls).

import { api, type AccessMode, type AccessOperation } from './api.ts';
import { EMPTY_ACCESS, type Access } from './domain.ts';

/** The resource the protected first task needs (catalog of the access module on the server). */
export const ACCESS_RESOURCE = { id: 'repo-learning-read', title: 'Репозиторий учебного проекта · чтение' };

/** The latest request for the first-task resource, or an empty state. */
export function accessFromServer(ops: AccessOperation[], reason = ''): Access {
  const op = ops.find(o => o.resourceId === ACCESS_RESOURCE.id);
  if (!op) return { ...EMPTY_ACCESS, resource: ACCESS_RESOURCE.title, reason };
  return { operationId: op.id, number: op.externalNumber, status: op.status, resource: ACCESS_RESOURCE.title, reason };
}

export function sameAccess(a: Access, b: Access): boolean {
  return a.operationId === b.operationId && a.number === b.number && a.status === b.status && a.resource === b.resource;
}

export type AccessKeys = { draft: string; confirm: string };

/**
 * Sends the request. The same keys make a retry safe: the server returns the same draft and the
 * same operation instead of a second request. Change the keys only when the reason changes.
 */
export async function submitAccessRequest(assignmentId: string, reason: string, keys: AccessKeys): Promise<AccessOperation> {
  const draft = await api.createAccessDraft({ assignmentId, resourceId: ACCESS_RESOURCE.id, reason }, keys.draft);
  return api.confirmAccess(draft.id, draft.version, reason, keys.confirm);
}

/** The mock's answer for the next requests, from the states panel option. */
export function accessMode(outcome: 'approved' | 'denied' | 'unknown'): AccessMode {
  return outcome === 'denied' ? 'deny' : outcome === 'unknown' ? 'lost' : 'approve';
}

export function accessStatusText(a: Access): string {
  switch (a.status) {
    case 'approved': return `Доступ одобрен${a.number ? ' · ' + a.number : ''}. Первая задача открыта.`;
    case 'denied': return 'В доступе отказано. Обсуди причину с бадди.';
    case 'unknown': return 'Ответ сервиса доступов пока не получен. Операция сохранена — сверим её без повторной заявки.';
    case 'pending': return `Заявка ${a.number ?? ''} на согласовании.`.replace('  ', ' ');
    default: return 'Заявки ещё нет.';
  }
}
