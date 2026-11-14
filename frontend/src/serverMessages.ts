// The server feed (listMessages) mapped into the prototype's per-intern runtime, so the existing
// message cards, checks and suggestions keep working. Used only when VITE_API=on.

import { makePlan, type Block, type Plan, type Role } from './domain.ts';
import { checkId, emptyRuntime, type AppState, type FeedEntry, type Runtime, type Snapshot } from './scenarios.ts';
import { pickQuestions, suggestionStage, validSelection, type QuestionSelection } from './suggestions.ts';
import type { ApiMessage, Assignment, FeedPage, SessionContext } from './api.ts';
import type { ChatMessage } from './domain.ts';
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

const moscowTime = (iso: string) => new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit', timeZone: 'Europe/Moscow' }).format(new Date(iso));

/** A question or an answer of the personal chat, as the chat bubbles show it. */
export function chatFromServer(m: ApiMessage): ChatMessage {
  return {
    id: m.id, role: m.kind === 'user' ? 'user' : 'assistant', text: m.text, time: moscowTime(m.createdAt),
    ...(m.answerRequestId ? { answerId: m.answerRequestId } : {}),
    ...(m.errorCode ? { error: true } : {}),
    ...(m.citations?.length ? { citations: m.citations.map(c => {
      const title = c.title.trim();
      // the section path starts with the document title: show only the part inside it
      const fragment = c.fragment.trim() === title ? '' : c.fragment.trim().replace(title + ' / ', '');
      return { title, fragment, ...(c.url ? { url: c.url } : {}) };
    }) } : {}),
  };
}

/** Questions still waiting for an answer: the answer job ids, oldest first. */
export function pendingAnswers(feed: FeedPage): string[] {
  const answered = new Set(feed.items.filter(m => m.kind === 'assistant' && m.answerRequestId).map(m => m.answerRequestId));
  return feed.items.filter(m => m.kind === 'user' && m.answerRequestId && !answered.has(m.answerRequestId)).map(m => m.answerRequestId!);
}

/**
 * Replace the checkpoint part of the intern's runtime with the server feed:
 * server messages become snapshots and feed entries in the server's order, together with the personal chat
 * (questions and answers are stored on the server too),
 * checks are the server's. Suggestions for the intern follow the latest delivered block.
 */
export function applyServerFeed(state: AppState, internId: string, role: Role, a: Assignment, feed: FeedPage, today: string, random: () => number = Math.random, serverSuggestions?: QuestionSelection): AppState {
  const runtime: Runtime = state.runtimes[internId] ?? emptyRuntime();
  const checkpoints = feed.items.filter(m => m.kind === 'checkpoint' && m.block);
  const months = feed.activePlan?.months ?? 6;
  const blocks = checkpoints.map(m => blockFromServer(m.block!, months));
  const plan = namesPlan(a, feed, blocks);

  const snapshots: Record<string, Snapshot> = {};
  checkpoints.forEach((m, i) => {
    snapshots[m.id] = { scenarioId: m.scenarioId ?? '', block: blocks[i], plan, ...(m.next ? { next: nextStub(m.next) } : {}) };
  });
  const feedEntries: FeedEntry[] = feed.items.flatMap((m): FeedEntry[] =>
    m.kind === 'checkpoint' ? (m.block ? [{ kind: 'checkpoint', id: m.id }] : []) : [{ kind: 'chat', message: chatFromServer(m) }]);

  const checks: Record<string, boolean> = {};
  for (const c of feed.checks) checks[checkId(role, c.messageId, c.taskId)] = c.checked;

  let suggestions = runtime.suggestions;
  if (role === 'intern' && serverSuggestions) suggestions = serverSuggestions; // getSuggestions: stored on the server
  else if (role === 'intern' && feed.activePlan) {
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
