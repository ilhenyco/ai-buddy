// HTTP client for the Хомыч backend (docs/architecture/openapi.yaml).
// Shared by all screens: add your module's calls to `api` below.
//
// Enabled with VITE_API=on. Without it the prototype keeps working on localStorage.
// The session is an HttpOnly cookie set by the server; mutations carry X-CSRF-Token.

export const API_ENABLED = import.meta.env?.VITE_API === 'on';
const BASE = '/api/v1';

// ---------- contract types (components.schemas) ----------

export type Person = { id: string; displayName: string };
export type Assignment = { id: string; intern: Person; buddy: Person; lead: Person; teamId: string; capabilities: string[] };
// features: enabled feature flags of the stand (ai_chat, access_integration, mcp_server, events_publisher).
export type SessionContext = { subject: Person; assignments: Assignment[]; capabilities: string[]; features?: string[]; serverTime: string; csrfToken: string };

export type ApiTask = {
  id: string; title: string; description: string; kind: string; link: string;
  url?: string; actions?: { buddy?: string; lead?: string };
};
export type ApiBlock = {
  id: string; stageKind?: string; title: string; date: string; endDate?: string;
  intern: string; buddy: string; lead: string; tasks: ApiTask[];
};
export type ApiPlan = {
  name: string; internId: string | null; buddyId: string | null; leadId: string | null;
  teamId: string; start: string; months: 6 | 9; blocks: ApiBlock[];
};
export type Revision = { id: string; number: number; publishedAt: string; plan: ApiPlan };
export type ApiScenario = {
  id: string; assignmentId: string | null; draft: ApiPlan; draftVersion: number; published: Revision | null;
  isActive: boolean; hasUnpublishedChanges: boolean; updatedAt: string;
};
export type ScenarioPage = { items: ApiScenario[]; nextCursor: string | null; counts: { all: number; drafts: number; published: number } };
export type Publication = { scenario: ApiScenario; replacedScenarioId: string | null; cutoffDate: string | null; activeRevisionId: string };

// messages (contract + two extensions: Message.next and FeedPage.activePlan, see backend/README.md)
export type ApiMessage = {
  id: string; assignmentId: string; kind: 'checkpoint' | 'user' | 'assistant'; recipientUserId: string; createdAt: string; text: string;
  scenarioId?: string; revisionId?: string; block?: ApiBlock; next?: { id: string; title: string; date: string };
  // personal chat: sources of an answer, its AnswerJob, the code of an answer that failed
  citations?: ApiCitation[]; answerRequestId?: string; errorCode?: string;
};
export type ApiCitation = { sourceId: string; sourceVersionId: string; title: string; fragment: string; url?: string };
// team materials (sources): every save is a version indexed by the knowledge service in the background
export type ApiSource = {
  id: string; versionId: string | null; title: string; body: string; category: string;
  acl: { teamIds: string[]; allowedRoleKinds?: string[] }; status: 'ready' | 'indexing' | 'error' | 'excluded'; updatedAt: string; etag: string;
};
export type SourceInput = { title: string; body: string; category: string; acl: { teamIds: string[] } };
export type IndexJob = { id: string; sourceId: string; candidateVersionId: string; status: 'queued' | 'running' | 'ready' | 'failed'; activeVersionId: string | null; error: { code: string; message: string } | null };
export type AnswerJob = {
  id: string; assignmentId: string; questionMessageId: string; status: 'queued' | 'running' | 'completed' | 'failed';
  answer: ApiMessage | null; error: { code: string; message: string; requestId: string } | null; createdAt: string;
};
export type ApiCheck = { messageId: string; taskId: string; userId: string; checked: boolean; updatedAt: string };
export type FeedPage = {
  items: ApiMessage[]; nextCursor: string | null; checks: ApiCheck[]; serverTime: string;
  activePlan: { scenarioId: string; revisionId: string; start: string; months: 6 | 9 } | null;
};
export type ApiNotification = { id: string; messageId: string; assignmentId: string; title: string; createdAt: string; deliveryStatus: string };
export type ClockState = { today: string; shifted: boolean; created: number };

// access requests (OQ08): the server stores the operation first, then calls the access service
export type AccessStatus = 'pending' | 'approved' | 'denied' | 'unknown';
export type AccessDraft = { id: string; assignmentId: string; resourceId: string; reason: string; version: number; expiresAt: string };
export type AccessOperation = {
  id: string; assignmentId: string; resourceId: string; correlationId: string;
  externalNumber: string | null; status: AccessStatus; submissionConfirmed: boolean; updatedAt: string;
};
export type ProtectedTask = { id: string; title: string; description: string; requiredResourceId: string };
export type AccessMode = 'approve' | 'deny' | 'lost';

// ---------- errors ----------

export class ApiError extends Error {
  status: number;
  code: string;
  details: Record<string, unknown>;
  requestId: string;
  constructor(status: number, code: string, message: string, details: Record<string, unknown> = {}, requestId = '') {
    super(message);
    this.status = status; this.code = code; this.details = details; this.requestId = requestId;
  }
  get reason(): string { return typeof this.details.reason === 'string' ? this.details.reason : ''; }
  get fields(): string[] { return Array.isArray(this.details.fields) ? this.details.fields as string[] : []; }
}

// ---------- transport ----------

let csrfToken = '';
type Fetch = typeof fetch;
let fetchImpl: Fetch = (...args) => fetch(...args);

/** For tests: replace the network. */
export function setFetch(f: Fetch) { fetchImpl = f; }

export function newKey() {
  return typeof crypto !== 'undefined' && 'randomUUID' in crypto ? crypto.randomUUID() : String(Date.now()) + Math.random();
}

async function request<T>(method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<T> {
  let res: Response;
  try {
    res = await fetchImpl(BASE + path, {
      method,
      credentials: 'same-origin',
      headers: {
        ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
        ...(method !== 'GET' && csrfToken ? { 'X-CSRF-Token': csrfToken } : {}),
        ...headers,
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch {
    throw new ApiError(0, 'NETWORK', 'Сервер недоступен. Проверьте соединение и попробуйте ещё раз.');
  }
  if (res.status === 204) return undefined as T;
  let data: any = null;
  try { data = await res.json(); } catch { /* empty or non-JSON body */ }
  if (!res.ok) {
    throw new ApiError(res.status, data?.code ?? 'HTTP_' + res.status, data?.message ?? 'Ошибка сервера.', data?.details ?? {}, data?.requestId ?? '');
  }
  return data as T;
}

const ifMatch = (version: number) => ({ 'If-Match': `"${version}"` });

// ---------- operations ----------

export const api = {
  // demo login (DEMO_MODE on the server, instead of SSO)
  demoUsers: () => request<{ items: Person[] }>('GET', '/demo/users'),
  async login(userId: string) {
    const ctx = await request<SessionContext>('POST', '/demo/session', { userId });
    csrfToken = ctx.csrfToken;
    return ctx;
  },
  async logout() {
    await request<void>('DELETE', '/demo/session');
    csrfToken = '';
  },
  /** Restores the session after a page reload (cookie is still there, CSRF token is not). */
  async session() {
    const ctx = await request<SessionContext>('GET', '/session/context');
    csrfToken = ctx.csrfToken;
    return ctx;
  },

  // scenarios
  listScenarios: (q = '', filter: 'all' | 'drafts' | 'published' = 'all', limit = 100) =>
    request<ScenarioPage>('GET', `/scenarios?limit=${limit}&filter=${filter}${q ? '&q=' + encodeURIComponent(q) : ''}`),
  getScenario: (id: string) => request<ApiScenario>('GET', `/scenarios/${encodeURIComponent(id)}`),
  createScenario: (body: { assignmentId: string | null; months: 6 | 9; start: string; name: string }, key = newKey()) =>
    request<ApiScenario>('POST', '/scenarios', body, { 'Idempotency-Key': key }),
  saveDraft: (id: string, version: number, plan: ApiPlan) =>
    request<ApiScenario>('PUT', `/scenarios/${encodeURIComponent(id)}/draft`, { plan }, ifMatch(version)),
  publish: (id: string, version: number, expectedActiveScenarioId: string | null, confirmReplacement: boolean, key = newKey()) =>
    request<Publication>('POST', `/scenarios/${encodeURIComponent(id)}/publications`,
      { draftVersion: version, expectedActiveScenarioId, confirmReplacement },
      { ...ifMatch(version), 'Idempotency-Key': key }),

  // messages by the calendar
  listMessages: (assignmentId: string, limit = 200, cursor?: string) =>
    request<FeedPage>('GET', `/assignments/${encodeURIComponent(assignmentId)}/messages?limit=${limit}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  /** The whole history: checkpoints and the personal chat can exceed one page. */
  async listAllMessages(assignmentId: string): Promise<FeedPage> {
    let page = await api.listMessages(assignmentId);
    const items = [...page.items];
    for (let n = 0; page.nextCursor && n < 20; n++) {
      page = await api.listMessages(assignmentId, 200, page.nextCursor);
      items.push(...page.items);
    }
    return { ...page, items, nextCursor: null };
  },
  setCheck: (messageId: string, taskId: string, checked: boolean) =>
    request<ApiCheck>('PUT', `/messages/${encodeURIComponent(messageId)}/tasks/${encodeURIComponent(taskId)}/check`, { checked }),
  listNotifications: () => request<{ items: ApiNotification[]; nextCursor: string | null }>('GET', '/notifications'),

  // demo clock (DEMO_MODE): move "today" for the scheduler, null returns to real time
  getClock: () => request<ClockState>('GET', '/demo/clock'),
  setClock: (date: string | null) => request<ClockState>('POST', '/demo/clock', { date }),
  // personal questions: the question and its job are stored at once, the answer comes later
  askQuestion: (assignmentId: string, text: string, key = newKey()) =>
    request<AnswerJob>('POST', `/assignments/${encodeURIComponent(assignmentId)}/questions`, { text }, { 'Idempotency-Key': key }),
  getAnswer: (id: string) => request<AnswerJob>('GET', `/answers/${encodeURIComponent(id)}`),
  listSources: (q = '') => request<{ items: ApiSource[]; nextCursor: string | null }>('GET', `/sources?limit=100${q ? `&q=${encodeURIComponent(q)}` : ''}`),
  getSource: (id: string) => request<ApiSource>('GET', `/sources/${encodeURIComponent(id)}`),
  createSource: (body: SourceInput, key = newKey()) => request<IndexJob>('POST', '/sources', body, { 'Idempotency-Key': key }),
  updateSource: (id: string, etag: string, body: SourceInput, key = newKey()) =>
    request<IndexJob>('PUT', `/sources/${encodeURIComponent(id)}`, body, { 'Idempotency-Key': key, 'If-Match': `"${etag}"` }),
  excludeSource: (id: string, etag: string, key = newKey()) =>
    request<ApiSource>('POST', `/sources/${encodeURIComponent(id)}/exclude`, undefined, { 'Idempotency-Key': key, 'If-Match': `"${etag}"` }),
  /** Three questions of the current stage, stored on the server: stable after answers and reloads. */
  getSuggestions: (assignmentId: string) =>
    request<{ stageKey: string; stageKind: string; poolVersion: string; questions: string[]; selectedAt: string }>('GET', `/assignments/${encodeURIComponent(assignmentId)}/suggestions`),
  /** Deletes everything done on the stand: demo scenarios again, real date, due messages sent. */
  resetDemo: () => request<ClockState>('POST', '/demo/reset'),

  // access requests: draft -> explicit confirmation by the person -> status reconciled by correlationId
  createAccessDraft: (body: { assignmentId: string; resourceId: string; reason: string }, key = newKey()) =>
    request<AccessDraft>('POST', '/access-drafts', body, { 'Idempotency-Key': key }),
  confirmAccess: (draftId: string, draftVersion: number, reason: string, key = newKey()) =>
    request<AccessOperation>('POST', `/access-drafts/${encodeURIComponent(draftId)}/confirm`,
      { draftVersion, reason, confirmed: true }, { 'Idempotency-Key': key }),
  getAccessOperation: (id: string) => request<AccessOperation>('GET', `/access-operations/${encodeURIComponent(id)}`),
  listAccessOperations: (assignmentId: string) =>
    request<{ items: AccessOperation[] }>('GET', `/assignments/${encodeURIComponent(assignmentId)}/access-operations`),
  getProtectedTask: (assignmentId: string) =>
    request<ProtectedTask>('GET', `/assignments/${encodeURIComponent(assignmentId)}/protected-first-task`),
  // demo only: what the access mock answers to the next requests
  setAccessMode: (mode: AccessMode) => request<{ mode: AccessMode }>('POST', '/demo/access-mode', { mode }),
};

/** A short message for the UI, by error code from the contract. */
export function errorText(e: unknown): string {
  if (!(e instanceof ApiError)) return 'Что-то пошло не так. Попробуйте ещё раз.';
  if (e.status === 412) return 'Черновик изменён в другой вкладке или другим человеком. Загрузите актуальную версию.';
  if (e.reason === 'REPLACEMENT_NOT_CONFIRMED') return 'У стажёра уже есть опубликованный сценарий. Подтвердите замену.';
  if (e.reason === 'ACTIVE_SCENARIO_CHANGED') return 'Действующий сценарий стажёра изменился. Обновите список и проверьте выбор.';
  if (e.reason === 'PUBLISHED_INTERN_LOCKED') return 'У опубликованного сценария нельзя сменить стажёра.';
  if (e.reason === 'DRAFT_EXPIRED') return 'Черновик заявки устарел. Отправьте заявку ещё раз.';
  if (e.status === 401) return 'Сессия закончилась. Войдите снова.';
  return e.message;
}
