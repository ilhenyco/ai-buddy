// "Мой план" panel: the delivered stages of the plan without the chat around them.
// Pure data: built from the same snapshots and checks as the feed, so marks stay in sync.

import { personalizeText, tasksForRole, type Block, type LinkKind, type Plan, type Role, type Task } from './domain.ts';
import { checkId, type Runtime } from './scenarios.ts';

export type PanelTask = { id: string; title: string; done: boolean; link: string; kind: LinkKind; url?: string };
export type PanelStage = {
  checkpointId: string; title: string; date: string; plan: Plan;
  tasks: PanelTask[]; done: number; total: number; current: boolean;
};
export type PanelLink = { key: string; label: string; kind: LinkKind; url?: string; plan: Plan; stage: string; task: string };
export type PlanPanel = {
  stages: PanelStage[];          // newest first, the current one is first
  current: PanelStage | null;
  links: PanelLink[];            // every link of the delivered stages once, in the order they came
  upcoming: { title: string; date: string }[];
  done: number; total: number;
};

/** Stages addressed to this role: the intern gets all, buddy and lead only stages with their text and tasks. */
function forRole(block: Block, role: Role): boolean {
  if (role === 'intern') return true;
  if (role === 'hr') return false;
  return !!block[role].trim() && tasksForRole(block, role).length > 0;
}

export function buildPlanPanel(runtime: Runtime, role: Role): PlanPanel {
  const delivered = Object.entries(runtime.snapshots)
    .filter(([, s]) => forRole(s.block, role))
    .sort(([, a], [, b]) => a.block.date.localeCompare(b.block.date) || a.block.id.localeCompare(b.block.id));

  const stages: PanelStage[] = delivered.map(([checkpointId, s], i) => {
    const tasks = tasksForRole(s.block, role).map((t: Task) => ({
      id: t.id, title: personalizeText(t.title, s.plan), link: t.link, kind: t.kind,
      ...(t.url ? { url: t.url } : {}),
      done: !!runtime.checks[checkId(role, checkpointId, t.id)],
    }));
    const done = tasks.filter(t => t.done).length;
    return { checkpointId, title: s.block.title, date: s.block.date, plan: s.plan, tasks, done, total: tasks.length, current: i === delivered.length - 1 };
  });

  const links: PanelLink[] = [];
  const seen = new Set<string>();
  for (const st of stages) for (const t of st.tasks) {
    const key = t.url ? 'url:' + t.url : t.kind + ':' + t.link;
    if (!t.link.trim() || seen.has(key)) continue;
    seen.add(key);
    links.push({ key, label: t.link, kind: t.kind, ...(t.url ? { url: t.url } : {}), plan: st.plan, stage: st.title, task: t.title });
  }

  // what comes next: the rest of the plan if it is known, otherwise the "next" of the last message
  const last = delivered.at(-1)?.[1];
  let upcoming: { title: string; date: string }[] = [];
  if (last) {
    const after = last.plan.blocks.filter(b => b.date > last.block.date && forRole(b, role))
      .sort((a, b) => a.date.localeCompare(b.date)).map(b => ({ title: b.title, date: b.date }));
    upcoming = after.length ? after : last.next ? [{ title: last.next.title, date: last.next.date }] : [];
  }

  return {
    stages: [...stages].reverse(),
    current: stages.at(-1) ?? null,
    links, upcoming,
    done: stages.reduce((n, s) => n + s.done, 0),
    total: stages.reduce((n, s) => n + s.total, 0),
  };
}
