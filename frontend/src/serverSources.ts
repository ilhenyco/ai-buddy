// Team materials in server mode: the server's sources mapped to the materials page.
import type { ApiSource } from './api.ts';
import type { Source } from './domain.ts';

/** Links stored with indexed materials point into the app: "https://homych.invalid/sources/<id>". */
export const SOURCE_LINK = 'https://homych.invalid/sources/';

export function sourceIdFromLink(url: string | undefined): string | null {
  return url && url.startsWith(SOURCE_LINK) ? url.slice(SOURCE_LINK.length) || null : null;
}

const updatedLabel = (iso: string) =>
  new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit', timeZone: 'Europe/Moscow' }).format(new Date(iso));

export function sourceFromServer(s: ApiSource): Source {
  return { id: s.id, title: s.title, body: s.body, category: s.category, team: s.acl.teamIds[0] ?? '', status: s.status, updated: updatedLabel(s.updatedAt) };
}

/** Materials still being indexed: the page checks them again soon. */
export const anyIndexing = (items: ApiSource[]) => items.some(s => s.status === 'indexing');
