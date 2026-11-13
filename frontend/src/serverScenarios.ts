// Mapping between the server's scenarios (ids, openapi.yaml) and the prototype's
// local AppState (names, src/scenarios.ts). Used only when VITE_API=on.
// The rest of the UI keeps working on AppState, so screens did not have to change.

import { makePlan, type Block, type LinkKind, type Plan, type Task } from './domain.ts';
import type { AppState, Scenario } from './scenarios.ts';
import type { ApiBlock, ApiPlan, ApiScenario, SessionContext } from './api.ts';

const SERVER_TASK_KINDS = new Set(['team', 'buddy', 'lead', 'hr', 'access', 'task', 'courses', 'practice', 'midpoint', 'final', 'handoff']);
const STAGE_KINDS = new Set(['start', 'week', 'adaptation', 'practice', 'midpoint', 'final-prep', 'final', 'after', 'generic']);

/** People the current user may see, by id (from session context). */
export function directory(ctx: SessionContext): Map<string, string> {
  const m = new Map<string, string>([[ctx.subject.id, ctx.subject.displayName]]);
  for (const a of ctx.assignments) for (const p of [a.intern, a.buddy, a.lead]) m.set(p.id, p.displayName);
  return m;
}

/** Interns of the lead's assignments, as the local intern list. */
export function internsOf(ctx: SessionContext): { id: string; name: string }[] {
  const seen = new Set<string>();
  return ctx.assignments.filter(a => !seen.has(a.intern.id) && seen.add(a.intern.id)).map(a => ({ id: a.intern.id, name: a.intern.displayName }));
}

/** The local UI role for the signed-in user. The server still checks every action. */
export function roleOf(ctx: SessionContext): 'intern' | 'buddy' | 'lead' | 'hr' {
  if (ctx.capabilities.includes('scenarios:write')) return 'lead';
  if (ctx.assignments.some(a => a.buddy.id === ctx.subject.id)) return 'buddy';
  if (ctx.assignments.some(a => a.intern.id === ctx.subject.id)) return 'intern';
  return 'hr';
}

// Visual-only block fields (eyebrow, process) are not part of the contract: take them from the template.
function templateBlock(id: string, months: 6 | 9): Block | undefined {
  const blocks = makePlan(months).blocks;
  return blocks.find(b => b.id === id) ?? blocks.find(b => id.startsWith(b.id + '-'));
}

function taskToServer(t: Task) {
  const actions = Object.fromEntries(Object.entries(t.actions ?? {}).filter(([, v]) => typeof v === 'string' && v.trim()));
  return {
    id: t.id, title: t.title, description: t.description, link: t.link,
    kind: SERVER_TASK_KINDS.has(t.kind) ? t.kind : 'team',
    ...(t.url?.trim() ? { url: t.url.trim() } : {}),
    ...(Object.keys(actions).length ? { actions } : {}),
  };
}

function blockToServer(b: Block): ApiBlock {
  return {
    id: b.id, ...(b.stageKind && STAGE_KINDS.has(b.stageKind) ? { stageKind: b.stageKind } : {}),
    title: b.title, date: b.date, ...(b.endDate ? { endDate: b.endDate } : {}),
    intern: b.intern, buddy: b.buddy, lead: b.lead, tasks: b.tasks.map(taskToServer),
  };
}

/** Local draft -> SaveDraft.plan. Buddy, lead and team are bound by the server from the assignment. */
export function planToServer(p: Plan, interns: { id: string; name: string }[]): ApiPlan {
  return {
    name: p.name,
    internId: interns.find(i => i.name === p.intern)?.id ?? null,
    buddyId: null, leadId: null,
    teamId: p.team || 'default',
    start: p.start, months: p.months,
    blocks: p.blocks.map(blockToServer),
  };
}

export function planFromServer(p: ApiPlan, people: Map<string, string>, version = 0): Plan {
  const name = (id: string | null) => (id ? people.get(id) ?? '' : '');
  return {
    name: p.name, intern: name(p.internId), buddy: name(p.buddyId), lead: name(p.leadId),
    team: p.teamId, start: p.start, months: p.months, version,
    blocks: p.blocks.map(b => blockFromServer(b, p.months)),
  };
}

export function blockFromServer(b: ApiBlock, months: 6 | 9 = 6): Block {
  const tpl = templateBlock(b.id, months);
  return {
    id: b.id, stageKind: b.stageKind as Block['stageKind'], title: b.title, date: b.date, endDate: b.endDate ?? '',
    eyebrow: tpl?.eyebrow ?? '', process: tpl?.process ?? '',
    intern: b.intern, buddy: b.buddy, lead: b.lead,
    tasks: b.tasks.map(t => ({
      id: t.id, title: t.title, description: t.description, link: t.link, kind: t.kind as LinkKind,
      ...(t.url ? { url: t.url } : {}), ...(t.actions ? { actions: t.actions } : {}),
    })),
  };
}

export function scenarioFromServer(s: ApiScenario, people: Map<string, string>): Scenario {
  return {
    id: s.id,
    internId: s.draft.internId,
    draft: planFromServer(s.draft, people, s.published?.number ?? 0),
    ...(s.published ? { published: planFromServer(s.published.plan, people, s.published.number) } : {}),
    updatedAt: s.updatedAt,
  };
}

/**
 * Replace the scenario part of the local state with the server's list.
 * Message history, checks and sources stay local until their modules are connected.
 * `cutoffs` keeps replacement cutoff dates learnt from publications (the list does not return them).
 */
export function applyServerScenarios(state: AppState, ctx: SessionContext, items: ApiScenario[], cutoffs: Record<string, string | null> = {}): AppState {
  const people = directory(ctx);
  const interns = internsOf(ctx);
  const activeScenarios: AppState['activeScenarios'] = {};
  for (const s of items) {
    if (s.isActive && s.draft.internId) activeScenarios[s.draft.internId] = { scenarioId: s.id, afterDate: cutoffs[s.id] ?? null };
  }
  return {
    ...state,
    interns: interns.length ? interns : state.interns,
    scenarios: items.map(s => scenarioFromServer(s, people)),
    activeScenarios,
  };
}

/** Field paths from a 422 answer, in words for the builder banner. */
export function describeFields(fields: string[]): string {
  if (!fields.length) return '';
  const labels: Record<string, string> = { name: 'название', internId: 'стажёр', start: 'дата начала', months: 'длительность' };
  const words = fields.slice(0, 4).map(f => {
    if (labels[f]) return labels[f];
    const m = /^blocks\[(\d+)\](?:\.tasks\[(\d+)\])?\.?(\w*)/.exec(f);
    if (!m) return f;
    const where = `блок ${Number(m[1]) + 1}` + (m[2] !== undefined ? `, пункт ${Number(m[2]) + 1}` : '');
    const what: Record<string, string> = { title: 'название', date: 'дата', intern: 'сообщение', url: 'ссылка', id: 'идентификатор' };
    return `${where}: ${what[m[3]] ?? m[3]}`;
  });
  return 'Проверьте: ' + words.join('; ') + (fields.length > 4 ? ' и ещё ' + (fields.length - 4) : '') + '.';
}
