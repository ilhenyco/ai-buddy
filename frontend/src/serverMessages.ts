// The server feed (listMessages) mapped into the prototype's per-intern runtime, so the existing
// message cards, checks and suggestions keep working. Used only when VITE_API=on.

import { makePlan, type Block, type Plan, type Role } from './domain.ts';
import { checkId, emptyRuntime, type AppState, type FeedEntry, type Runtime, type Snapshot } from './scenarios.ts';
import { pickQuestions, suggestionStage, validSelection } from './suggestions.ts';
import type { Assignment, FeedPage, SessionContext } from './api.ts';
import { blockFromServer } from './serverScenarios.ts';

/** The subject's assignment for the intern shown on screen. */
export function assignmentFor(ctx: SessionContext, internId: string): Assignment | undefined {
  return ctx.assignments.find(a => a.intern.id === internId);
}

/** Calendar date of the server in Moscow (the scheduler's calendar). */
export function serverToday(serverTime: string): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Moscow' }).format(new Date(serverTime));
}

// Names for personalised texts ("У Саши", "с Дашей") come from the assignment.
function namesPlan(a: Assignment, feed: FeedPage, blocks: Block[]): Plan {
  return {
    ...makePlan(feed.activePlan?.months ?? 6),
    name: '', intern: a.intern.displayName, buddy: a.buddy.displayName, lead: a.lead.displayName,
    team: a.teamId, start: feed.activePlan?.start ?? '', blocks,
  };
}

function nextStub(next: NonNullable<FeedPage['items'][number]['next']>): Block {
  return { id: next.id, title: next.title, date: next.date, endDate: '', eyebrow: '', process: '', intern: '', buddy: '', lead: '', tasks: [] };
}

/**
 * Replace the checkpoint part of the intern's runtime with the server feed:
 * server messages become snapshots and feed entries (new ones are appended, so local AI chat stays in place),
 * checks are the server's. Suggestions for the intern follow the latest delivered block.
 */
export function applyServerFeed(state: AppState, internId: string, role: Role, a: Assignment, feed: FeedPage, today: string, random: () => number = Math.random): AppState {
  const runtime: Runtime = state.runtimes[internId] ?? emptyRuntime();
  const checkpoints = feed.items.filter(m => m.kind === 'checkpoint' && m.block);
  const months = feed.activePlan?.months ?? 6;
  const blocks = checkpoints.map(m => blockFromServer(m.block!, months));
  const plan = namesPlan(a, feed, blocks);

  const snapshots: Record<string, Snapshot> = {};
  checkpoints.forEach((m, i) => {
    snapshots[m.id] = { scenarioId: m.scenarioId ?? '', block: blocks[i], plan, ...(m.next ? { next: nextStub(m.next) } : {}) };
  });
  const serverIds = new Set(checkpoints.map(m => m.id));
  const kept: FeedEntry[] = runtime.feed.filter(e => e.kind === 'chat' || serverIds.has(e.id));
  const known = new Set(kept.filter(e => e.kind === 'checkpoint').map(e => (e as { id: string }).id));
  const feedEntries = [...kept, ...checkpoints.filter(m => !known.has(m.id)).map(m => ({ kind: 'checkpoint' as const, id: m.id }))];

  const checks: Record<string, boolean> = {};
  for (const c of feed.checks) checks[checkId(role, c.messageId, c.taskId)] = c.checked;

  let suggestions = runtime.suggestions;
  if (role === 'intern' && feed.activePlan) {
    const stage = suggestionStage({ ...plan, blocks }, today);
    if (!validSelection(suggestions, stage)) {
      const previous = Array.isArray(suggestions?.questions) ? suggestions!.questions : [];
      suggestions = { ...stage, questions: pickQuestions(stage.topic, random, previous) };
    }
  }

  const next: Runtime = { ...runtime, feed: feedEntries, snapshots, checks, ...(suggestions ? { suggestions } : {}) };
  if (JSON.stringify(next) === JSON.stringify(runtime)) return state; // nothing new: no re-render, no save
  return { ...state, runtimes: { ...state.runtimes, [internId]: next } };
}
