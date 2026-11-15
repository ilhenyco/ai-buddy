import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { ListChecks, ArrowUpRight, ArrowLeft, ArrowRight, ArrowUp, ArrowDown, Bell, BellSlash, BookOpen, CalendarBlank, CaretDown, Check, CheckCircle, ChatCircle, ClipboardText, Clock, Copy, DotsThree, FileText, FolderSimple, GearSix, Info, LinkSimple, LockSimple, MagnifyingGlass, PaperPlaneRight, Plus, SidebarSimple, SignOut, SlidersHorizontal, SquaresFour, Trash, Users, WarningCircle, Monitor, DeviceMobile, CloudArrowUp } from '@phosphor-icons/react';
import { Avatar, Badge, Modal, Spinner, ExternalLabel } from './ui';
import { makePlan, tasksForRole, shiftPlanStart, validExternalUrl, roleNames, people, dateLabel, timeLabel, demoAnswer, visibleSources, roleBody, personalizeText, submitAccess, EMPTY_ACCESS, type Access, type Block, type ChatMessage, type LinkKind, type Page, type Plan, type Role, type Source } from './domain';
import './prototype.css';
import './bot.css';
import './origination.css';
import './scenarios.css';
import './planPanel.css';
import './product.css';
import { STORE, LEGACY_STORE, clone, restoreState, initialState, persistState, latestState, emptyRuntime, checkId, hasDraft, listScenarios, addScenario, patchScenario, publicationError, publishScenario, deliverDue, updateRuntime, type AppState, type ScenarioFilter } from './scenarios';
import { ensureSuggestions, suggestionStage, validSelection, type QuestionSelection, type SuggestionTopic } from './suggestions';
import { API_ENABLED, api, errorText, ApiError, newKey, type ApiNotification, type ProtectedTask, type SessionContext } from './api';
import { applyServerScenarios, describeFields, directory, internsOf, planToServer, roleOf, scenarioFromServer } from './serverScenarios';
import { Login } from './Login';
import { applyServerFeed, assignmentFor, pendingAnswers, serverToday } from './serverMessages';
import { buildPlanPanel } from './planPanel';
import { anyIndexing, sourceFromServer, sourceIdFromLink } from './serverSources';
import { ACCESS_RESOURCE, accessFromServer, accessMode, accessStatusText, sameAccess, submitAccessRequest, type AccessKeys } from './serverAccess';
const SESSION_USER_KEY='homych.session.user';

function localDate(at:number){const d=new Date(at);return `${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')}`;}

function Empty({title,text,icon,action,onAction}:{title:string;text:string;icon:ReactNode;action?:string;onAction?:()=>void}){return <div className="empty-state"><div className="empty-icon">{icon}</div><h2>{title}</h2><p>{text}</p>{action&&onAction&&<button className="button button-primary" onClick={onAction}>{action}</button>}</div>;}

export function App() {
  const [restored]=useState(()=>restoreState(localStorage));
  const [state,setLocalState]=useState(restored.state);
  const stateRef=useRef(state);
  const unsaved=useRef(false);
  const [viewInternId,setViewInternId]=useState(restored.state.interns[0].id);
  const [selectedScenarioId,setSelectedScenarioId]=useState(restored.state.scenarios[0]?.id??'');
  const [scenarioSearch,setScenarioSearch]=useState('');
  const [scenarioFilter,setScenarioFilter]=useState<ScenarioFilter>('all');
  const [saveError,setSaveError]=useState(restored.error);
  const [lastSavedAt,setLastSavedAt]=useState('');
  const restoreBlocked=useRef(!!restored.error);
  // Server mode (VITE_API=on): session, draft versions for If-Match, cutoffs learnt from publications.
  const [session,setSession]=useState<SessionContext|null>(null);
  // Server feed of the intern on screen: whether a plan is published, and the scheduler's "today".
  const [serverFeed,setServerFeed]=useState<{internId:string;hasPlan:boolean;today:string}|null>(null);
  const pickInternWithPlan=useRef(false); // right after login: show an intern who already has a plan
  const [sessionChecked,setSessionChecked]=useState(!API_ENABLED);
  const [serverStatus,setServerStatus]=useState<'idle'|'saving'|'saved'|'error'>('idle');
  const versions=useRef<Record<string,number>>({});
  const cutoffs=useRef<Record<string,string|null>>({});
  const pendingSave=useRef<string|null>(null);
  const saveTimer=useRef<ReturnType<typeof setTimeout>|null>(null);
  const saveChain=useRef<Promise<boolean>>(Promise.resolve(true));
  const [role,setRole]=useState<Role>('intern'); const [page,setPage]=useState<Page>('messages');
  const [mode,setMode]=useState('normal'); const [clockTick,setClockTick]=useState(Date.now());
  const [mobile,setMobile]=useState(false); const [theme,setTheme]=useState('light');
  const [modal,setModal]=useState<string|null>(null); const [toast,setToast]=useState('');const [toastError,setToastError]=useState(false);
  const [sound,setSound]=useState(false); const [input,setInput]=useState('');const [busyInterns,setBusyInterns]=useState<Record<string,boolean>>({});
  // server mode: questions waiting for an answer (stored on the server, survive a reload)
  const [pendingIds,setPendingIds]=useState<string[]>([]);const [asking,setAsking]=useState(false);
  const busy=API_ENABLED&&session?asking||pendingIds.length>0:!!busyInterns[viewInternId];
  const [sourceSearch,setSourceSearch]=useState('');
  const [editId,setEditId]=useState('week');const [editRole,setEditRole]=useState<'intern'|'buddy'|'lead'>('intern');
  const [previewRole,setPreviewRole]=useState<Role>('intern'); const [formError,setFormError]=useState('');
  const [access,setAccess]=useState<Access>(EMPTY_ACCESS);const [reason,setReason]=useState('Для работы над первой задачей команды');
  const [accessOutcome,setAccessOutcome]=useState<'approved'|'denied'|'unknown'>('approved');
  const [directDemo,setDirectDemo]=useState(false); const [contactRole,setContactRole]=useState<Role>('buddy');
  // server mode: the access request goes to the access service (mock) through the backend
  const [protectedTask,setProtectedTask]=useState<ProtectedTask|null>(null);const [accessBusy,setAccessBusy]=useState(false);const accessKeys=useRef<AccessKeys|null>(null);const protectedTaskFor=useRef('');
  const highlightTimer=useRef<ReturnType<typeof setTimeout>|null>(null); // "Показать в чате" from the plan tab
  // server mode: the bell lists the server's notifications; the dot is for ones newer than the last look
  const [notes,setNotes]=useState<ApiNotification[]>([]);const [notesSeen,setNotesSeen]=useState('');
  const knownNotes=useRef<Set<string>|null>(null);const pendingScroll=useRef<string|null>(null);const notesBefore=useRef('');
  const [contactPlan,setContactPlan]=useState<Plan|null>(null);
  const [sourceId,setSourceId]=useState<string|null>(null); const [sourceDraft,setSourceDraft]=useState('');
  const [sourceTitle,setSourceTitle]=useState('');const [uploadName,setUploadName]=useState('');
  const [sourceError,setSourceError]=useState('');const [editingSourceId,setEditingSourceId]=useState<string|null>(null);const [pendingSourceIds,setPendingSourceIds]=useState<string[]>([]);
  // server mode: materials live on the server; the etag guards concurrent edits
  const [sourceCategory,setSourceCategory]=useState('');const sourceEtags=useRef<Record<string,string>>({});const [sourceBusy,setSourceBusy]=useState(false);
  const clockInput=useRef<HTMLInputElement>(null);const fileInput=useRef<HTMLInputElement>(null);const chatEnd=useRef<HTMLDivElement>(null);const chatScroll=useRef<HTMLDivElement>(null);const toastTimer=useRef<ReturnType<typeof setTimeout>|null>(null);
  const selectedScenario=state.scenarios.find(s=>s.id===selectedScenarioId);
  const viewIntern=state.interns.find(i=>i.id===viewInternId)??state.interns[0];
  const activeScenario=state.scenarios.find(s=>s.id===state.activeScenarios[viewIntern.id]?.scenarioId);
  const fallbackPlan={...makePlan(),intern:viewIntern.name};
  const plan=activeScenario?.published??fallbackPlan;
  const draft=selectedScenario?.draft??fallbackPlan;
  // server mode: the feed is per signed-in user (two tabs with different roles must not overwrite each other)
  const runtimeKey=API_ENABLED&&session?`${session.subject.id}@${viewIntern.id}`:viewIntern.id;
  const runtime=state.runtimes[runtimeKey]??emptyRuntime();
  const serverMode=API_ENABLED&&!!session;
  // feature flags of the stand (FEATURES on the server); an older server without the list has everything on
  const featureOn=(f:string)=>!serverMode||!session?.features||session.features.includes(f);
  const today=serverMode&&serverFeed?serverFeed.today:state.demoDate??localDate(clockTick);
  const planPublished=serverMode?serverFeed?.internId===viewIntern.id&&serverFeed.hasPlan:!!activeScenario?.published;
  const suggestions=serverMode?(planPublished?runtime.suggestions?.questions??[]:[]):activeScenario?.published&&validSelection(runtime.suggestions,suggestionStage(plan,today))?runtime.suggestions!.questions:[];
  const delivered=Object.values(runtime.snapshots).sort((a,b)=>a.block.date.localeCompare(b.block.date));
  const ownDelivered=delivered.filter(s=>role==='intern'||(role!=='hr'&&tasksForRole(s.block,role).length>0&&!!s.block[role].trim()));
  const snapshot=ownDelivered.at(-1);
  const block=snapshot?.block??plan.blocks[0];
  const messagePlan=snapshot?.plan??plan;
  const next=snapshot?.next;
  const messages=runtime.feed.filter(e=>e.kind==='chat');
  const unreadNotes=serverMode?notes.filter(n=>n.createdAt>notesSeen).length:0;
  const draftBlock=draft.blocks.find(b=>b.id===editId)??draft.blocks[0];
  const publicSources=(API_ENABLED&&session?state.sources.filter(s=>s.status!=='excluded'):visibleSources(state.sources)).map((s,i)=>mode==='source-error'&&i===0?{...s,status:'error' as const}:pendingSourceIds.includes(s.id)?{...s,status:'indexing' as const}:s);
  const activeSource=state.sources.find(s=>s.id===sourceId);

  function saveCurrent(showNotice=false){
    if(restoreBlocked.current)return false;
    const error=persistState(localStorage,stateRef.current);unsaved.current=!!error;setSaveError(error);
    if(!error){setLastSavedAt(new Date().toISOString());if(showNotice)notify('Черновик сохранён в этом браузере');}
    return !error;
  }
  function setState(value:AppState|((current:AppState)=>AppState)){
    let current=stateRef.current;
    if(!unsaved.current&&!restoreBlocked.current)current=latestState(localStorage,current);
    const next=typeof value==='function'?value(current):value;
    if(next===current){if(current!==stateRef.current){stateRef.current=current;setLocalState(current);}return;}
    stateRef.current=next;setLocalState(next);saveCurrent();
  }
  useEffect(()=>{
    // Read on mount without echoing an existing snapshot into other tabs.
    if(!restoreBlocked.current){try{if(localStorage.getItem(STORE)){const disk=restoreState(localStorage);if(!disk.error){stateRef.current=disk.state;setLocalState(disk.state);setLastSavedAt(new Date().toISOString());}}else saveCurrent();}catch{/* Recovery banner already describes unavailable storage. */}}
    const handler=(event:StorageEvent)=>{
      if(event.key!==STORE||!event.newValue||restoreBlocked.current||unsaved.current)return;
      const restored=restoreState(localStorage);
      if(!restored.error&&JSON.stringify(stateRef.current)!==JSON.stringify(restored.state)){stateRef.current=restored.state;setLocalState(restored.state);setLastSavedAt(new Date().toISOString());}
    };
    window.addEventListener('storage',handler);return()=>window.removeEventListener('storage',handler);
  },[]);
  useEffect(()=>{if(!saveError)return;const handler=(e:BeforeUnloadEvent)=>{e.preventDefault();};window.addEventListener('beforeunload',handler);return()=>window.removeEventListener('beforeunload',handler);},[saveError]);
  useEffect(()=>{if(API_ENABLED)return; /* server mode: the date comes from the server */ const timer=setInterval(()=>setClockTick(Date.now()),15_000);return()=>clearInterval(timer);},[]);
  useEffect(()=>{
    if(mode==='no-plan')return;
    if(API_ENABLED&&session)return; // the server scheduler sends messages
    setState(current=>ensureSuggestions(deliverDue(current,today),today));
  },[state.scenarios,state.activeScenarios,today,mode,viewInternId,role,session]);
  useEffect(()=>{chatEnd.current?.scrollIntoView({behavior:'auto',block:'end'});},[runtime.feed.length,busy,role,page,viewInternId]);
  const previousFeedCount=useRef(runtime.feed.length);
  useEffect(()=>{if(runtime.feed.length>previousFeedCount.current)beep();previousFeedCount.current=runtime.feed.length;},[runtime.feed.length]);
  useEffect(()=>()=>{if(toastTimer.current)clearTimeout(toastTimer.current);},[]);
  function notify(text:string,error=false){setToastError(error);setToast(text);if(toastTimer.current)clearTimeout(toastTimer.current);toastTimer.current=setTimeout(()=>setToast(''),3600);}
  function beep(){
    if(!sound)return;
    try{const ctx=new AudioContext();const osc=ctx.createOscillator();const gain=ctx.createGain();osc.connect(gain);gain.connect(ctx.destination);osc.frequency.value=720;gain.gain.setValueAtTime(.025,ctx.currentTime);gain.gain.exponentialRampToValueAtTime(.001,ctx.currentTime+.18);osc.start();osc.stop(ctx.currentTime+.18);osc.onended=()=>void ctx.close();}catch{/* Sound depends on browser permissions. */}
  }
  function changeRole(value:Role){setRole(value);setPage('messages');setInput('');}
  function setCheck(event:string,taskId:string,value:boolean){
    const key=checkId(role,event,taskId),internId=runtimeKey;
    setState(s=>updateRuntime(s,internId,r=>({...r,checks:{...r.checks,[key]:value}})));
    if(API_ENABLED&&session)api.setCheck(event,taskId,value).catch(async e=>{
      setState(s=>updateRuntime(s,internId,r=>({...r,checks:{...r.checks,[key]:!value}})));
      if(e instanceof ApiError&&[401,403,404].includes(e.status)&&await switchIfSignedInElsewhere())return;
      notify(errorText(e),true);
    });
  }
  function openAccess(form=false){
    if((form||!access.operationId)&&!featureOn('access_integration')){notify('Заявки на доступ на этом стенде отключены');return;}
    setModal(form?'access-form':access.operationId?'access-status':'access-form');
  }
  async function askServer(q:string){
    const a=session&&assignmentFor(session,viewInternId);if(!a)return;
    setInput('');setAsking(true);
    try{const job=await api.askQuestion(a.id,q);setPendingIds(ids=>ids.includes(job.id)?ids:[...ids,job.id]);await syncFeed();}
    catch(e){setInput(q);notify(errorText(e),true);} // the text goes back to the field: nothing is lost
    finally{setAsking(false);}
  }
  // waiting for an answer: ask the server about the job every 2 s, then reload the history
  useEffect(()=>{
    if(!serverMode||!pendingIds.length)return;
    const timer=setInterval(async()=>{
      try{const jobs=await Promise.all(pendingIds.map(id=>api.getAnswer(id)));if(jobs.some(j=>j.status==='completed'||j.status==='failed'))await syncFeed();}
      catch{/* the next tick tries again; the regular sync also brings the answer */}
    },2000);
    return ()=>clearInterval(timer);
  },[serverMode,pendingIds.join(),viewInternId]);
  function send(question=input){
    if(!question.trim()||busy||role!=='intern')return;
    if(serverMode){void askServer(question.trim());return;}
    const q=question.trim();setInput('');const busyKey=viewInternId;setBusyInterns(current=>({...current,[busyKey]:true}));
    const user:ChatMessage={id:crypto.randomUUID(),role:'user',text:q,time:timeLabel()};
    const recipient=runtimeKey;
    setState(s=>updateRuntime(s,recipient,r=>({...r,feed:[...r.feed,{kind:'chat',message:user}]})));
    setTimeout(()=>{
      const answer=demoAnswer(q,state.sources,mode,plan);
      const message:ChatMessage={id:crypto.randomUUID(),role:'assistant',time:timeLabel(),...answer};
      setState(s=>updateRuntime(s,recipient,r=>({...r,feed:[...r.feed,{kind:'chat',message}]})));setBusyInterns(current=>({...current,[busyKey]:false})); // same key as above
    },650);
  }
  function openLink(kind:LinkKind,context=plan){
    if(kind==='intern'||kind==='buddy'||kind==='lead'||kind==='hr'){setContactPlan(context);setContactRole(kind);setModal('contact');return;}
    setModal(kind);
  }
  // ---------- server mode ----------
  async function loadScenarios(ctx=session,pickIntern=false){
    if(!ctx||roleOf(ctx)!=='lead')return;
    try{
      const page=await api.listScenarios();
      for(const s of page.items)versions.current[s.id]=s.draftVersion;
      setState(s=>applyServerScenarios(s,ctx,page.items,cutoffs.current));
      const interns=internsOf(ctx);
      // on login, show an intern who already has an active plan (the lead can switch above)
      const withPlan=interns.find(i=>page.items.some(s=>s.isActive&&s.draft.internId===i.id));
      setViewInternId(current=>pickIntern&&withPlan?withPlan.id:interns.some(i=>i.id===current)?current:interns[0]?.id??current);
    }catch(e){if(e instanceof ApiError&&e.status===401)setSession(null);else notify(errorText(e));}
  }
  function startSession(ctx:SessionContext){
    setSession(ctx);setSessionChecked(true);changeRole(roleOf(ctx));setState(s=>({...s,runtimes:{}})); // another user's copies never stay in this browser
    pickInternWithPlan.current=roleOf(ctx)==='buddy';
    try{localStorage.setItem(SESSION_USER_KEY,ctx.subject.id);}catch{/* only a hint for other tabs */}
    const interns=internsOf(ctx);
    if(interns.length){setState(s=>({...s,interns}));setViewInternId(interns[0].id);}
    void loadScenarios(ctx,true);
  }
  /** Another tab signed in as someone else: switch this tab to that user and say so. */
  async function switchIfSignedInElsewhere():Promise<boolean>{
    try{
      const now=await api.session();
      if(now.subject.id===session?.subject.id)return false;
      startSession(now);notify(`В другой вкладке выполнен вход: ${now.subject.displayName}. Эта вкладка переключена.`,true);return true;
    }catch{setSession(null);notify('Сессия закончилась. Войдите снова.',true);return true;}
  }
  // the browser keeps nothing personal after logout: messages, chat and checks are reloaded from the server
  async function logout(){try{await api.logout();}catch{/* the cookie expires anyway */}setSession(null);setState(s=>({...s,runtimes:{}}));try{localStorage.setItem(SESSION_USER_KEY,'');}catch{/* hint only */}}
  useEffect(()=>{
    if(!API_ENABLED)return;
    const onStorage=(e:StorageEvent)=>{
      if(e.key!==SESSION_USER_KEY)return;
      if(!e.newValue){setSession(null);return;}
      if(e.newValue!==session?.subject.id)api.session().then(startSession).catch(()=>setSession(null));
    };
    window.addEventListener('storage',onStorage);return()=>window.removeEventListener('storage',onStorage);
  },[session]);
  useEffect(()=>{if(API_ENABLED)api.session().then(startSession).catch(()=>setSessionChecked(true));},[]);
  useEffect(()=>{if(API_ENABLED&&page==='scenarios')void flushSave().then(()=>loadScenarios());},[page]); // unsaved edits first, then the fresh list
  function saveToServer(id:string):Promise<boolean>{
    const run=async()=>{
      const scenario=stateRef.current.scenarios.find(s=>s.id===id);
      if(!scenario||versions.current[id]===undefined)return true;
      setServerStatus('saving');
      try{
        const saved=await api.saveDraft(id,versions.current[id],planToServer(scenario.draft,stateRef.current.interns));
        versions.current[id]=saved.draftVersion;
        const people=directory(session!);const name=(v:string|null)=>v?people.get(v)??'':'';
        setState(s=>({...s,scenarios:s.scenarios.map(x=>x.id===id?{...x,internId:saved.draft.internId,updatedAt:saved.updatedAt,
          draft:{...x.draft,buddy:name(saved.draft.buddyId),lead:name(saved.draft.leadId),team:saved.draft.teamId}}:x)}));
        setServerStatus('saved');setLastSavedAt(saved.updatedAt);return true;
      }catch(e){
        setServerStatus('error');
        setFormError(errorText(e)+(e instanceof ApiError&&e.fields.length?' '+describeFields(e.fields):''));
        return false;
      }
    };
    saveChain.current=saveChain.current.then(run,run); // one save at a time: each uses the version of the previous one
    return saveChain.current;
  }
  function scheduleSave(id:string){
    pendingSave.current=id;setServerStatus('saving');
    if(saveTimer.current)clearTimeout(saveTimer.current);
    saveTimer.current=setTimeout(()=>{const target=pendingSave.current;pendingSave.current=null;if(target)void saveToServer(target);},700);
  }
  function flushSave():Promise<boolean>{
    if(saveTimer.current)clearTimeout(saveTimer.current);
    const target=pendingSave.current;pendingSave.current=null;
    return target?saveToServer(target):saveChain.current;
  }
  async function syncFeed(){
    const ctx=session;if(!API_ENABLED||!ctx||role==='hr')return;
    const internId=viewInternId;const a=assignmentFor(ctx,internId);
    if(!a){setServerFeed({internId,hasPlan:false,today:localDate(Date.now())});return;}
    try{
      const feed=await api.listAllMessages(a.id);const day=serverToday(feed.serverTime);
      if(pickInternWithPlan.current){
        pickInternWithPlan.current=false;
        if(!feed.activePlan)for(const other of ctx.assignments.filter(x=>x.id!==a.id)){
          const page=await api.listMessages(other.id,1);
          if(page.activePlan){setViewInternId(other.intern.id);return;} // the effect re-syncs for this intern
        }
      }
      // the cookie belongs to someone else now (login in another tab): switch this tab to that user
      if(feed.items.some(m=>m.recipientUserId!==ctx.subject.id)){const fresh=await api.session();startSession(fresh);return;}
      let serverSuggestions:QuestionSelection|undefined;
      if(role==='intern'&&feed.activePlan&&featureOn('ai_chat'))try{const sel=await api.getSuggestions(a.id);serverSuggestions={stageKey:sel.stageKey,topic:sel.stageKind as SuggestionTopic,questions:sel.questions};}catch{/* local pick as a fallback */}
      setState(s=>applyServerFeed(s,`${ctx.subject.id}@${internId}`,role,a,feed,day,Math.random,serverSuggestions));
      const waiting=role==='intern'?pendingAnswers(feed):[];
      setPendingIds(prev=>prev.join()===waiting.join()?prev:waiting);
      void syncNotes(ctx.subject.id);
      try{const ops=await api.listAccessOperations(a.id);applyAccess(a.id,accessFromServer(ops.items,reason));}
      catch{/* the access module is optional: messages keep working without it */}
      const hasPlan=!!feed.activePlan;
      // no re-render when nothing changed: idle polling must not touch the screen
      setServerFeed(prev=>prev&&prev.internId===internId&&prev.hasPlan===hasPlan&&prev.today===day?prev:{internId,hasPlan,today:day});
      if(mode==='network-error')setMode('normal');
      if(pendingScroll.current)setTimeout(()=>{const id=pendingScroll.current;if(id&&document.getElementById('cp-'+id)){pendingScroll.current=null;showInChat(id);}},60);
    }catch(e){if(e instanceof ApiError&&e.status===401)setSession(null);}
  }
  const seenKey=(userId:string)=>'homych.notifications.seen.'+userId;
  async function syncNotes(userId:string){
    try{
      const page=await api.listNotifications();const items=page.items;
      if(knownNotes.current){ // a new stage arrived while the app is open: say so, like a push
        const fresh=items.filter(n=>!knownNotes.current!.has(n.id));
        if(fresh.length){notify(fresh.length===1?`Хомыч прислал: ${fresh[0].title.replace(/^Хомыч:\s*/,'')}`:`Хомыч прислал новых сообщений: ${fresh.length}`);if(sound)beep();}
      }else{try{setNotesSeen(localStorage.getItem(seenKey(userId))??'');}catch{/* no storage: every note is new */}}
      knownNotes.current=new Set(items.map(n=>n.id));
      setNotes(prev=>prev.length===items.length&&prev.every((n,i)=>n.id===items[i].id)?prev:items);
    }catch{/* the bell is optional */}
  }
  function openNotifications(){
    setModal('notifications');notesBefore.current=notesSeen;
    if(serverMode&&notes.length){const latest=notes.reduce((m,n)=>n.createdAt>m?n.createdAt:m,'');setNotesSeen(latest);try{localStorage.setItem(seenKey(session!.subject.id),latest);}catch{/* hint only */}}
  }
  function openNote(n:ApiNotification){
    setModal(null);
    const a=session?.assignments.find(x=>x.id===n.assignmentId);
    if(a&&a.intern.id!==viewInternId){pendingScroll.current=n.messageId;selectIntern(a.intern.id);return;}
    showInChat(n.messageId);
  }
  async function resetDemo(){
    try{
      const st=await api.resetDemo();
      knownNotes.current=null;setAccess(EMPTY_ACCESS);setProtectedTask(null);setModal(null);
      try{localStorage.removeItem(STORE);localStorage.removeItem(LEGACY_STORE);}catch{/* nothing stored */}
      setState(initialState());setPage('messages');
      await loadScenarios();await syncFeed();
      notify(`Демо начато заново: ${dateLabel(st.today,true)}, сообщений разослано: ${st.created}`);
    }catch(e){notify(errorText(e),true);}
  }
  useEffect(()=>{
    if(!API_ENABLED||!session)return;
    void syncFeed();const timer=setInterval(()=>void syncFeed(),5000); // new messages appear without a reload
    return()=>clearInterval(timer);
  },[session,viewInternId,role]);
  async function setServerClock(date:string|null){
    try{
      const st=await api.setClock(date);await syncFeed();
      const own=Object.values(stateRef.current.runtimes[runtimeKey]?.snapshots??{}).sort((x,y)=>x.block.date.localeCompare(y.block.date));
      const next=own.at(-1)?.next;
      notify(`Дата на сервере: ${dateLabel(st.today,true)} — `+(st.created?`пришло новых сообщений: ${st.created}`:next?`новых этапов нет, следующий «${next.title}» ${dateLabel(next.date)}`:'новых этапов нет'));
    }
    catch(e){notify(errorText(e),true);}
  }
  async function sendAccessRequest(){
    const a=session&&assignmentFor(session,viewInternId);if(!a)return;
    accessKeys.current??={draft:newKey(),confirm:newKey()}; // a retry after a failure reuses the keys: no second request
    setAccessBusy(true);
    try{
      const op=await submitAccessRequest(a.id,reason.trim(),accessKeys.current);accessKeys.current=null;
      applyAccess(a.id,accessFromServer([op],reason));setModal('access-status');
      notify(op.status==='unknown'?'Заявка сохранена, ответ сервиса пока не получен':`Заявка ${op.externalNumber} отправлена в сервис доступов`);
    }catch(e){notify(errorText(e),true);}
    finally{setAccessBusy(false);}
  }
  async function checkAccessStatus(){
    const a=session&&assignmentFor(session,viewInternId);if(!a||!access.operationId)return;
    try{const next=accessFromServer([await api.getAccessOperation(access.operationId)],access.reason);applyAccess(a.id,next);notify(accessStatusText(next));}
    catch(e){notify(errorText(e),true);}
  }
  async function changeAccessOutcome(outcome:'approved'|'denied'|'unknown'){
    setAccessOutcome(outcome);if(!serverMode)return;
    try{await api.setAccessMode(accessMode(outcome));notify(outcome==='approved'?'Сервис доступов одобрит новые заявки':outcome==='denied'?'Сервис доступов откажет в новых заявках':'Сервис доступов «потеряет» ответ на новую заявку');}
    catch(e){notify(errorText(e),true);}
  }
  // another user or another intern on screen: their request is loaded by the next sync
  useEffect(()=>{if(serverMode){setAccess(EMPTY_ACCESS);setProtectedTask(null);accessKeys.current=null;protectedTaskFor.current='';setPendingIds([]);}},[runtimeKey]);
  /** Shows the request of this assignment; the protected task comes from the server only with an approved access. */
  function applyAccess(assignmentId:string,next:Access){
    setAccess(prev=>sameAccess(prev,next)?prev:next);
    if(next.status!=='approved'){protectedTaskFor.current='';setProtectedTask(null);}
    else if(protectedTaskFor.current!==assignmentId+'/'+next.operationId){
      protectedTaskFor.current=assignmentId+'/'+next.operationId;
      api.getProtectedTask(assignmentId).then(setProtectedTask,()=>setProtectedTask(null));
    }
  }
  async function reloadDraft(){setFormError('');await loadScenarios();notify('Загружена актуальная версия с сервера');}
  async function createServerScenario(){
    try{
      const created=await api.createScenario({assignmentId:null,months:6,start:today,name:'Новый сценарий'});
      versions.current[created.id]=created.draftVersion;
      const local=scenarioFromServer(created,directory(session!));
      setState(s=>({...s,scenarios:[...s.scenarios,local]}));openScenario(created.id);
    }catch(e){notify(errorText(e),true);}
  }
  async function publishToServer(){
    const scenario=selectedScenario;if(!scenario)return;
    if(!(await flushSave())){setModal(null);return;}
    const current=scenario.internId?stateRef.current.activeScenarios[scenario.internId]?.scenarioId??null:null;
    try{
      const pub=await api.publish(scenario.id,versions.current[scenario.id],current,!!current&&current!==scenario.id);
      cutoffs.current[pub.scenario.id]=pub.cutoffDate;versions.current[pub.scenario.id]=pub.scenario.draftVersion;
      setModal(null);await loadScenarios();notify('Сценарий опубликован на сервере и используется для сообщений');
    }catch(e){
      setModal(null);setFormError(errorText(e)+(e instanceof ApiError&&e.fields.length?' '+describeFields(e.fields):''));
      if(e instanceof ApiError&&(e.status===412||e.status===409))void loadScenarios();
    }
  }
  function saveStatusText(){
    if(!API_ENABLED)return saveError?'Изменения не сохранены':lastSavedAt?`Сохранено в браузере · ${new Date(lastSavedAt).toLocaleTimeString('ru-RU')}`:'Сохраняем…';
    if(serverStatus==='saving')return 'Сохраняем на сервере…';
    if(serverStatus==='error')return 'Изменения не сохранены на сервере';
    return lastSavedAt?`Сохранено на сервере · ${new Date(lastSavedAt).toLocaleTimeString('ru-RU')}`:'Черновик на сервере';
  }
  // ---------- end of server mode ----------

  function updateDraft(patch:Partial<Plan>){
    if(API_ENABLED&&session&&patch.intern!==undefined){
      // Buddy, lead and team follow the chosen intern's assignment (the server binds them the same way).
      const a=session.assignments.find(x=>x.intern.displayName===patch.intern);
      patch={...patch,buddy:a?.buddy.displayName??'',lead:a?.lead.displayName??session.subject.displayName,...(a?{team:a.teamId}:{})};
    }
    setState(s=>patchScenario(s,selectedScenarioId,patch));setFormError('');if(API_ENABLED&&session)scheduleSave(selectedScenarioId);
  }
  function openScenario(id:string){setSelectedScenarioId(id);setEditId('week');setEditRole('intern');setFormError('');setPage('builder');}
  function createScenario(){if(API_ENABLED&&session){void createServerScenario();return;}const id=crypto.randomUUID();setState(s=>addScenario(s,id,today));openScenario(id);}
  function selectIntern(id:string){setViewInternId(id);setInput('');setAccess(EMPTY_ACCESS);setProtectedTask(null);setDirectDemo(false);setModal(null);setPage(p=>p==='plan'?'plan':'messages');}
  function updateBlock(patch:Partial<Block>){updateDraft({blocks:draft.blocks.map(b=>b.id===draftBlock.id?{...b,...patch}:b)});}
  function moveBlock(direction:number){
    const list=[...draft.blocks],i=list.findIndex(b=>b.id===draftBlock.id),j=i+direction;
    if(j<0||j>=list.length)return;
    [list[i],list[j]]=[list[j],list[i]];updateDraft({blocks:list});
  }
  function publish(){
    const error=publicationError(selectedScenario);
    if(error){setFormError(error);setModal(null);return;}
    if(API_ENABLED&&session){void publishToServer();return;}
    setState(s=>publishScenario(s,selectedScenarioId,today));setModal(null);notify('Сценарий опубликован и используется для сообщений');
  }
  async function loadSources():Promise<boolean>{
    try{
      const page=await api.listSources();
      for(const x of page.items)sourceEtags.current[x.id]=x.etag;
      const mapped=page.items.map(sourceFromServer);
      setState(s=>JSON.stringify(s.sources)===JSON.stringify(mapped)?s:{...s,sources:mapped});
      return anyIndexing(page.items);
    }catch(e){notify(errorText(e),true);return false;}
  }
  // the materials page: from the server; while something is being indexed, check every 3 s
  useEffect(()=>{
    if(!serverMode||page!=='knowledge')return;
    let stop=false;let timer:ReturnType<typeof setTimeout>;
    const tick=async()=>{const indexing=await loadSources();if(!stop)timer=setTimeout(tick,indexing?3000:15000);};
    void tick();
    return ()=>{stop=true;clearTimeout(timer);};
  },[serverMode,page]);
  async function saveServerSource(){
    const team=assignmentFor(session!,viewInternId)?.teamId??session!.assignments[0]?.teamId;
    if(!team)return;
    const body={title:sourceTitle.trim(),body:sourceDraft.trim(),category:sourceCategory.trim()||'Общее',acl:{teamIds:[team]}};
    setSourceBusy(true);
    try{
      if(editingSourceId)await api.updateSource(editingSourceId,sourceEtags.current[editingSourceId]??'1',body);else await api.createSource(body);
      setModal(null);notify(editingSourceId?'Материал сохранён. Пока новая версия готовится к поиску, ответы используют прежнюю.':'Материал добавлен. Он попадёт в ответы после индексации.');
      await loadSources();
    }catch(e){
      if(e instanceof ApiError&&e.status===412){setSourceError('Материал уже изменили в другом месте. Закрой окно и открой материал заново.');await loadSources();}
      else setSourceError(errorText(e));
    }finally{setSourceBusy(false);}
  }
  async function excludeSource(id:string){
    if(!serverMode){setState(st=>({...st,sources:st.sources.map(x=>x.id===id?{...x,status:'excluded'}:x)}));notify('Материал исключён из новых ответов');return;}
    try{await api.excludeSource(id,sourceEtags.current[id]??'1');notify('Материал исключён из новых ответов. История версий сохранена.');await loadSources();}
    catch(e){notify(errorText(e),true);await loadSources();}
  }
  /** A source of an answer: our material opens inside the app, an outside one in a new tab. */
  async function openCitation(url:string){
    const id=sourceIdFromLink(url);
    if(!id){window.open(url,'_blank','noopener,noreferrer');return;}
    try{const src=await api.getSource(id);const mapped=sourceFromServer(src);setState(s=>({...s,sources:[...s.sources.filter(x=>x.id!==id),mapped]}));setSourceId(id);setModal('source');}
    catch(e){notify(e instanceof ApiError&&e.status===404?'Этот материал больше не используется в ответах.':errorText(e),true);}
  }
  function saveSource(){
    if(!sourceTitle.trim()||!sourceDraft.trim()){setSourceError('Добавь название и текст материала.');return;}
    if(serverMode){void saveServerSource();return;}
    const existing=state.sources.find(s=>s.id===editingSourceId);
    const source:Source={id:existing?.id??'source-'+crypto.randomUUID(),title:sourceTitle.trim(),body:sourceDraft.trim(),category:existing?.category??'Новый материал',team:'platform',status:'ready',updated:'Только что'};
    if(existing)setPendingSourceIds(ids=>[...ids,source.id]);else setState(s=>({...s,sources:[...s.sources,{...source,status:'indexing'}]}));
    setModal(null);notify(existing?'Обновляем материал. До готовности используется прежняя версия.':'Материал добавлен. Подготавливаем его для поиска.');
    setTimeout(()=>{setState(s=>({...s,sources:s.sources.map(x=>x.id===source.id&&x.status!=='excluded'?source:x)}));setPendingSourceIds(ids=>ids.filter(id=>id!==source.id));},900);
  }
  const readFile=async(file:File|undefined)=>{
    if(!file)return;setUploadName(file.name);
    if(!/\.(txt|md)$/i.test(file.name)){setSourceError('В макете можно прочитать TXT или Markdown. Другие форматы требуют серверной обработки.');return;}
    if(file.size>1_000_000){setSourceError('Для демонстрации выбери текстовый файл до 1 МБ.');return;}
    setSourceDraft(await file.text());if(!sourceTitle)setSourceTitle(file.name.replace(/\.[^.]+$/,''));setSourceError('');
  };

  function Checklist({b,p,asRole=role,preview=false,checkpointId=b.id}:{b:Block;p:Plan;asRole?:Role;preview?:boolean;checkpointId?:string}){
    const items=tasksForRole(b,asRole);
    return <div className="bot-checklist" aria-label={'Чек-лист: '+roleNames[asRole]}>{items.map(t=><div className="bot-task" key={t.id}>
      <span className="check-control"><input id={`${asRole}-${checkpointId}-${t.id}${preview?'-preview':''}`} aria-label={t.title} type="checkbox" checked={preview?false:!!runtime.checks[checkId(asRole,checkpointId,t.id)]} disabled={preview} onChange={e=>setCheck(checkpointId,t.id,e.target.checked)}/>{!preview&&runtime.checks[checkId(asRole,checkpointId,t.id)]&&<Check size={16} weight="bold" aria-hidden="true"/>}</span>
      <div><label htmlFor={`${asRole}-${checkpointId}-${t.id}${preview?'-preview':''}`} className={!preview&&runtime.checks[checkId(asRole,checkpointId,t.id)]?'task-done':''}>{personalizeText(t.title,p)}</label>
      {asRole!=='intern'&&<p className="joint-task-context">{t.description}</p>}
      {!preview&&<button className="text-link" onClick={()=>{if(t.url&&validExternalUrl(t.url))window.open(t.url,'_blank','noopener,noreferrer');else openLink(t.kind,p);}}><ExternalLabel>{t.link}</ExternalLabel></button>}</div>
    </div>)}</div>;
  }

  function StageMessage({b,p,asRole=role,preview=false,upcoming,checkpointId}:{b:Block;p:Plan;asRole?:Role;preview?:boolean;upcoming?:Block;checkpointId?:string}){
    if(asRole!=='intern'&&(!b[asRole as 'buddy'|'lead']?.trim()||!tasksForRole(b,asRole).length))return preview?<p className="muted">Для этой роли участие в пунктах не предусмотрено.</p>:null;
    const n=upcoming??[...p.blocks].sort((a,b)=>a.date.localeCompare(b.date)).find(x=>x.date>b.date);
    return <article className="checkpoint-entry" aria-label={'Сообщение: '+b.title}>
      {!preview&&<div className="day-divider"><span>{dateLabel(b.date,true)}</span></div>}
      <div className="bot-label"><Avatar bot size={25}/><span>Хомыч</span></div>
      <div className={"message-card bot-checkpoint "+(asRole==='buddy'?'checkpoint-buddy':'')}><span className="checkpoint-tag">{asRole==='intern'?'По плану':'Помощь стажёру'}</span><h2>{b.title}</h2><p className="message-body">{roleBody(b,asRole,p)}</p>
      {Checklist({b,p,asRole,preview,checkpointId})}
      {n&&<p className="checkpoint-next">Дальше — {n.title.toLowerCase()}, {dateLabel(n.date)}. Я напомню.</p>}
      <div className="message-time">{preview?'Предпросмотр':'09:00'}</div></div>
    </article>;
  }

  function HelperTaskEditor(){return <section className="helper-task-editor"><h3>Совместные действия по пунктам стажёра</h3><p className="muted small">Переформулируй нужные пункты для этой роли. Пустое поле — участие не требуется.</p>{draftBlock.tasks.map((t,i)=><label key={t.id}>Пункт {i+1}: {t.title}<textarea rows={2} aria-label={`Действие ${editRole==='buddy'?'бадди':'руководителя'} по пункту ${i+1}`} value={t.actions?.[editRole as 'buddy'|'lead']??''} onChange={e=>updateBlock({tasks:draftBlock.tasks.map(x=>x.id===t.id?{...x,actions:{...x.actions,[editRole]:e.target.value}}:x)})}/></label>)}</section>;}

  function Scenarios(){
    const rows=listScenarios(state,scenarioSearch,scenarioFilter);
    const filters:[ScenarioFilter,string][]=[['all','Все'],['drafts','Черновики'],['published','Опубликованные']];
    return <div className="scenarios-page builder-page">
      <div className="page-heading"><div><p className="eyebrow">Сопровождение стажировки</p><h1>Сценарии стажёров</h1><p>Продолжи настройку черновика или создай новый вариант стажировки.</p></div><button className="button button-primary" onClick={createScenario}><Plus size={20}/>Создать сценарий</button></div>
      <div className="scenarios-toolbar"><label className="search-field"><MagnifyingGlass size={20}/><input type="search" aria-label="Поиск сценариев" placeholder="Название или стажёр" value={scenarioSearch} onChange={e=>setScenarioSearch(e.target.value)}/></label>
      <div className="tabs scenario-filters" role="tablist" aria-label="Статус сценария">{filters.map(([value,label])=><button key={value} role="tab" aria-selected={scenarioFilter===value} onClick={()=>setScenarioFilter(value)}>{label}<span>{listScenarios(state,'',value).length}</span></button>)}</div></div>
      <p className="muted small scenario-list-note">У каждого стажёра один вариант используется для сообщений. Опубликованный сценарий с изменениями также виден в «Черновиках».</p>
      {rows.length?<><div className="scenario-table-heading" aria-hidden="true"><span>Сценарий и стажёр</span><span>Период</span><span>Состояние</span><span>Изменён</span><span/></div><ul className="scenario-list" aria-label="Сценарии стажёров">{rows.map(s=>{
        const active=!!s.internId&&state.activeScenarios[s.internId]?.scenarioId===s.id;
        return <li key={s.id}><button className="scenario-row" onClick={()=>openScenario(s.id)} aria-label={`Открыть сценарий ${s.draft.name||'Без названия'} — ${s.draft.intern||'стажёр не выбран'}`}>
          <span className="scenario-title"><strong>{s.draft.name||'Без названия'}</strong><small><Users size={15}/>{s.draft.intern||'Стажёр не выбран'}</small></span>
          <span className="scenario-period"><strong>{s.draft.months} месяцев</strong><small>С {dateLabel(s.draft.start,true)}</small></span>
          <span className="scenario-status"><Badge tone={s.published?'blue':'gray'}>{s.published?'Опубликован':'Черновик'}</Badge>{s.published&&<small className={active?'scenario-active':''}>{active?'Используется для сообщений':'Не используется'}</small>}{s.published&&hasDraft(s)&&<small>Есть неопубликованные изменения</small>}</span>
          <span className="scenario-updated">{new Date(s.updatedAt).toLocaleString('ru-RU',{day:'numeric',month:'short',hour:'2-digit',minute:'2-digit'})}</span><ArrowRight size={21}/>
        </button></li>;
      })}</ul></>:<Empty title={scenarioSearch||scenarioFilter!=='all'?'Сценарии не найдены':'Создай первый сценарий'} text={scenarioSearch||scenarioFilter!=='all'?'Попробуй другой запрос или фильтр.':'Готовые блоки помогут настроить сообщения стажёру и его помощникам.'} icon={<ClipboardText size={40}/>} action={scenarioSearch||scenarioFilter!=='all'?'Сбросить поиск и фильтр':'Создать сценарий'} onAction={()=>{if(scenarioSearch||scenarioFilter!=='all'){setScenarioSearch('');setScenarioFilter('all');}else createScenario();}}/>}
    </div>;
  }

  function showInChat(checkpointId:string){
    setPage('messages');
    setTimeout(()=>{ // after the chat is mounted and scrolled to the end
      const el=document.getElementById('cp-'+checkpointId);if(!el){pendingScroll.current=checkpointId;return;}
      el.scrollIntoView({behavior:'smooth',block:'start'});el.classList.add('checkpoint-highlight');
      if(highlightTimer.current)clearTimeout(highlightTimer.current);
      highlightTimer.current=setTimeout(()=>el.classList.remove('checkpoint-highlight'),1800);
    },80);
  }
  function openPanelLink(kind:LinkKind,p:Plan,url?:string){if(url&&validExternalUrl(url))window.open(url,'_blank','noopener,noreferrer');else openLink(kind,p);}
  const TEAMS:Record<string,string>={platform:'Платформа',mobile:'Мобильная разработка'};
  const showRail=serverMode&&(role==='buddy'||role==='lead')&&state.interns.length>0;
  /** Buddy and lead: their interns on the left; the chat and the plan follow the chosen one. */
  function InternsRail(){
    const current=ownDelivered.at(-1)?.block;
    return <nav className="interns-rail" aria-label="Мои стажёры"><h2>Мои стажёры</h2><ul>{[...state.interns].sort((x,y)=>Number(!!state.activeScenarios[y.id])-Number(!!state.activeScenarios[x.id])).map(i=>{
      const a=session?.assignments.find(x=>x.intern.id===i.id);
      const fresh=a?notes.filter(n=>n.assignmentId===a.id&&n.createdAt>notesSeen).length:0;
      const selected=i.id===viewIntern.id;
      const status=selected&&planPublished?(current?`Сейчас: ${current.title}`:'План опубликован'):role==='lead'?(state.activeScenarios[i.id]?'План опубликован':'План не опубликован'):(a?TEAMS[a.teamId]??a.teamId:'');
      return <li key={i.id}><button className={'intern-card'+(selected?' intern-card-active':'')} aria-current={selected?'true':undefined} onClick={()=>{if(!selected)selectIntern(i.id);}}>
        <span className="intern-initials" aria-hidden="true">{i.name.split(' ').map(w=>w[0]).join('').slice(0,2)}</span>
        <span className="intern-card-text"><strong>{i.name}</strong><small>{status}</small></span>
        {fresh>0&&<span className="intern-fresh" aria-label={`новых: ${fresh}`}>{fresh}</span>}
      </button></li>;})}</ul></nav>;
  }
  function ProfileModal(){
    const me=session!.subject;const mine=session!.assignments.find(a=>a.intern.id===me.id);
    const helped=session!.assignments.find(a=>a.buddy.id===me.id||a.lead.id===me.id);
    const facts:[string,string][]=role==='intern'&&mine?[['Бадди',mine.buddy.displayName],['Руководитель',mine.lead.displayName],['Команда',TEAMS[mine.teamId]??mine.teamId]]
      :helped?[['Команда',TEAMS[helped.teamId]??helped.teamId],['Стажёров',String(state.interns.length)]]:[];
    return <Modal title="Профиль" onClose={()=>setModal(null)} footer={<button className="button" onClick={()=>{setModal(null);void logout();}}><SignOut size={17}/>Выйти</button>}>
      <div className="profile-head"><Avatar size={52}/><div><strong>{me.displayName}</strong><Badge tone="blue">{roleNames[role]}</Badge></div></div>
      {facts.length>0&&<section className="profile-section"><h3>Команда</h3><dl className="profile-facts">{facts.map(([k,v])=><div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}</dl></section>}
      <section className="profile-section"><h3>Настройки</h3>
        <div className="profile-setting"><span>Тема</span><div className="segmented" role="group" aria-label="Тема оформления"><button aria-pressed={theme==='light'} onClick={()=>setTheme('light')}>Светлая</button><button aria-pressed={theme==='dark'} onClick={()=>setTheme('dark')}>Тёмная</button></div></div>
        <div className="profile-setting"><span>Звук новых сообщений<small>Пока вкладка Хомыча открыта</small></span><input type="checkbox" role="switch" aria-label="Звук новых сообщений" checked={sound} onChange={e=>setSound(e.target.checked)}/></div>
      </section>
      <button className="profile-demo" onClick={()=>setModal('states')}><SlidersHorizontal size={20}/><span><strong>Демо-стенд</strong><small>Дата, сервис доступов, сброс. Только для показа</small></span><ArrowRight size={17}/></button>
    </Modal>;
  }
  function PlanView(){
    const panel=buildPlanPanel(runtime,role);const cur=panel.current;
    const pct=panel.total?Math.round(panel.done/panel.total*100):0;
    const accessLine=access.operationId?{approved:'Доступ к репозиторию одобрен',denied:'В доступе к репозиторию отказано',unknown:'Заявка на доступ: сверяем результат',pending:'Заявка на доступ на согласовании',none:''}[access.status]:'';
    return <div className="plan-view" aria-label="Мой план">
      <div className="plan-view-head"><h2>{role==='intern'?'Мой план':`План стажёра · ${plan.intern}`}</h2>
        {panel.total>0&&<div className="plan-progress"><div className="plan-progress-bar" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct} aria-label="Выполнено пунктов"><span style={{width:pct+'%'}}/></div><span>{panel.done} из {panel.total}</span></div>}</div>
      {!cur?<p className="muted small">Этапы появятся здесь, когда Хомыч пришлёт первое сообщение.</p>:<div className="plan-columns"><div className="plan-col-main">
      <section className="plan-section"><h3>Сейчас</h3><div className="plan-now">
        <strong>{cur.title}</strong><span className="plan-now-meta">С {dateLabel(cur.date,true)} · выполнено {cur.done} из {cur.total}</span>
        {accessLine&&<div className={'plan-now-row plan-access plan-access-'+access.status}><LockSimple size={16}/><button className="text-link" onClick={()=>setModal('access-status')}>{accessLine}{access.number?` · ${access.number}`:''}</button></div>}
        {panel.upcoming[0]&&<div className="plan-now-row"><CalendarBlank size={16}/><span>Дальше — {panel.upcoming[0].title.toLowerCase()}, {dateLabel(panel.upcoming[0].date)}</span></div>}
        <button className="plan-to-chat" onClick={()=>showInChat(cur.checkpointId)}>Показать в чате<ArrowRight size={14}/></button>
      </div></section>
      <section className="plan-section"><h3>Этапы</h3>{panel.stages.map(st=><details key={st.checkpointId} className="plan-stage" open={st.current}>
        <summary><span>{st.title}<small>{dateLabel(st.date,true)}</small></span><span className={'plan-stage-count'+(st.total&&st.done===st.total?' complete':'')}>{st.done}/{st.total}</span><CaretDown size={16}/></summary>
        <div className="plan-stage-body">{st.tasks.map(t=><div className="plan-task" key={t.id}><span className="check-control"><input id={`panel-${st.checkpointId}-${t.id}`} type="checkbox" checked={t.done} onChange={e=>setCheck(st.checkpointId,t.id,e.target.checked)}/>{t.done&&<Check size={16} weight="bold" aria-hidden="true"/>}</span><label htmlFor={`panel-${st.checkpointId}-${t.id}`} className={t.done?'task-done':''}>{t.title}</label></div>)}
        <button className="plan-to-chat" onClick={()=>showInChat(st.checkpointId)}>Показать в чате<ArrowRight size={14}/></button></div>
      </details>)}</section>
      </div><div className="plan-col-side">
      {panel.links.length>0&&<section className="plan-section"><h3>Ссылки и контакты</h3><div className="plan-links">{panel.links.map(l=><button key={l.key} className="plan-link" onClick={()=>openPanelLink(l.kind,l.plan,l.url)}><LinkSimple size={17}/><span>{l.label}<small>{l.stage} · {l.task}</small></span><ArrowRight size={15}/></button>)}</div></section>}
      {panel.upcoming.length>0&&<section className="plan-section"><h3>Впереди</h3><ul className="plan-upcoming">{panel.upcoming.slice(0,5).map(u=><li key={u.date+u.title}><span>{u.title}</span><time dateTime={u.date}>{dateLabel(u.date)}</time></li>)}{panel.upcoming.length>5&&<li><span>и ещё {panel.upcoming.length-5}</span></li>}</ul><p className="muted small">Подробности придут в чат в свой день.</p></section>}
      </div></div>}
    </div>;
  }
  function PlanPage(){
    return <div key="plan" className={'plan-page'+(showRail?' with-rail':'')}>{showRail&&InternsRail()}<div className="plan-scroll">{role==='hr'||!planPublished||mode==='no-plan'?<Empty title="План ещё не опубликован" text="Когда руководитель опубликует сценарий, здесь появятся этапы, ссылки и контакты." icon={<CalendarBlank size={40}/>} action="Перейти к сообщениям" onAction={()=>setPage('messages')}/>:PlanView()}</div></div>;
  }
  function Messages(){
    return <div key="messages" className={'workspace bot-workspace'+(showRail?' with-rail':'')}>{showRail&&InternsRail()}<section className="chat-pane"><header className="chat-header"><Avatar bot size={38}/><div className="chat-title"><h1>Хомыч</h1><p>{role==='intern'?'Твой AI-бадди':role==='hr'?'Организационные сведения · вариант':`Помощь стажёру · ${plan.intern}`}</p></div><button className="icon-button" title="Контакты команды" aria-label="Открыть контакты команды" onClick={()=>setModal('team-panel')}><Users size={22}/></button><button className="icon-button" aria-label="Настройки чата" onClick={()=>setModal('settings')}><DotsThree size={26}/></button></header>
    <div className="chat-content" ref={chatScroll}><div className="conversation bot-conversation">
    {(mode==='no-plan'||!planPublished)?(role==='lead'?<Empty title={`У стажёра ${plan.intern} нет опубликованного плана`} text="Создай сценарий или опубликуй готовый черновик — Хомыч начнёт писать по датам." icon={<CalendarBlank size={40}/>} action="Открыть сценарии" onAction={()=>setPage('scenarios')}/>:<Empty title="План ещё не опубликован" text="Руководитель готовит сценарий. Если старт уже близко, свяжись с ним." icon={<CalendarBlank size={40}/>} action="Контакт руководителя" onAction={()=>openLink('lead')}/>):mode==='network-error'?<Empty title="Не получилось загрузить сообщения" text="Проверь соединение и попробуй ещё раз. Сохранённые отметки останутся на месте." icon={<WarningCircle size={40}/>} action="Попробовать ещё раз" onAction={()=>{setMode('normal');notify('Сообщения загружены');}}/>:role==='hr'?<><div className="proposal-banner"><Info size={20}/><div><strong>Вариант для обсуждения</strong><p>Состав сообщений HR пока не согласован. Здесь показана передача организационных материалов.</p></div></div><Empty title="Организационные материалы" text="Курсы, памятки и контакты можно передать руководителю для сценария." icon={<FolderSimple size={40}/>} action="Посмотреть материалы" onAction={()=>setPage('knowledge')}/></>:<>
    {!ownDelivered.length&&<p className="waiting-message">Я напишу, когда наступит первый шаг стажировки.</p>}
    {runtime.feed.map(entry=>{
      if(entry.kind==='checkpoint'){const snap=runtime.snapshots[entry.id];return snap?<div key={entry.id} id={'cp-'+entry.id}>{StageMessage({b:snap.block,p:snap.plan,upcoming:snap.next,checkpointId:entry.id})}</div>:null;}
      if(role!=='intern')return null;
      const m=entry.message;return <div key={m.id} className={'chat-message '+(m.role==='user'?'user-message':'assistant-message')}><p>{m.text}</p>{m.source&&<button className="source-link" onClick={()=>{setSourceId(m.source!);setModal('source');}}><BookOpen size={15}/>{state.sources.find(s=>s.id===m.source)?.title}<ArrowRight size={14}/></button>}{m.citations?.length?<div className="answer-sources"><span>Источники</span>{m.citations.map((c,i)=>c.url&&validExternalUrl(c.url)?<a key={i} className="source-link" href={c.url} target="_blank" rel="noopener noreferrer" onClick={e=>{if(sourceIdFromLink(c.url)){e.preventDefault();void openCitation(c.url!);}}}><BookOpen size={15}/><span>{c.title}{c.fragment&&<small>{c.fragment}</small>}</span><ArrowUpRight size={14}/></a>:<span key={i} className="source-link"><BookOpen size={15}/><span>{c.title}{c.fragment&&<small>{c.fragment}</small>}</span></span>)}</div>:null}{m.error&&<button className="text-link" onClick={()=>openLink('buddy')}>Связаться с бадди</button>}<small>{m.time}</small></div>;
    })}
    {busy&&role==='intern'&&<div className="assistant-message thinking"><Spinner label={serverMode?'Ищу в материалах команды. Обычно это занимает до минуты — можно закрыть чат, ответ сохранится':'Ищу в материалах команды'}/></div>}<div ref={chatEnd}/></>}
    </div></div>
    {role==='intern'&&planPublished&&mode!=='no-plan'&&mode!=='network-error'&&featureOn('ai_chat')?<footer className="composer-area"><div className="suggestions" role="group" aria-label="Вопросы по текущему этапу">{suggestions.map(q=><button key={q} onClick={()=>send(q)} disabled={busy}>{q}</button>)}</div><form className="composer" onSubmit={e=>{e.preventDefault();send();}}><textarea aria-label="Сообщение Хомычу" placeholder="Спроси Хомыча" value={input} rows={1} onChange={e=>setInput(e.target.value)} onKeyDown={e=>{if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();send();}}}/><button className="send-button" aria-label="Отправить сообщение" disabled={!input.trim()||busy}><span>Спросить</span></button></form><p className="composer-note"><LockSimple size={12}/>Личный чат · ответы по материалам команды</p></footer>:<footer className="feed-footer"><Bell size={16}/>{role==='hr'?'Вариант для согласования':role==='intern'&&!featureOn('ai_chat')?'Вопросы Хомычу на этом стенде отключены. Хомыч напишет, когда наступит следующий шаг':'Хомыч напишет, когда наступит следующий шаг'}</footer>}</section></div>;
  }

  function Builder(){return <div key="builder" className="builder-page"><button className="text-link scenario-back" onClick={()=>setPage('scenarios')}><ArrowLeft size={18}/>К списку сценариев</button><div className="page-heading"><div><p className="eyebrow">Сопровождение стажировки</p><h1>Сценарий стажёра</h1><p>Собери путь из готовых блоков и настрой сообщения участникам.</p><p className="save-status" role="status">{saveStatusText()}</p></div><div className="heading-actions"><button className="button" onClick={()=>{if(API_ENABLED&&session){pendingSave.current=selectedScenarioId;void flushSave().then(ok=>{if(ok)notify('Черновик сохранён на сервере');});}else saveCurrent(true);}}>Сохранить черновик</button><button className="button button-primary" onClick={()=>{const error=publicationError(selectedScenario);setFormError(error);if(!error)setModal('publish');}}>Опубликовать</button></div></div>{formError&&<div role="alert" className="error-banner">{formError}{API_ENABLED&&serverStatus==='error'&&<button className="button" onClick={()=>void reloadDraft()}>Загрузить актуальную версию</button>}</div>}<div className="scenario-name-field"><label>Название сценария<input value={draft.name} onChange={e=>updateDraft({name:e.target.value})} placeholder="Например, адаптация backend-стажёра"/></label></div><div className="plan-settings"><label>Стажёр<select value={draft.intern} disabled={!!selectedScenario?.published} onChange={e=>updateDraft({intern:e.target.value})}><option value="">Выбери стажёра</option>{state.interns.map(i=><option key={i.id} value={i.name}>{i.name}</option>)}</select>{selectedScenario?.published&&<small>Стажёр закреплён за опубликованным сценарием</small>}</label><label>Бадди<select value={draft.buddy} onChange={e=>updateDraft({buddy:e.target.value})}><option>{people.buddy}</option><option>Игорь Волков</option></select></label><label>Длительность<select value={draft.months} onChange={e=>{const months=Number(e.target.value) as 6|9;const template=shiftPlanStart(makePlan(months),draft.start);updateDraft({months,blocks:draft.blocks.map(b=>{const fresh=template.blocks.find(x=>x.id===b.id);return fresh?{...b,date:fresh.date,endDate:fresh.endDate}:b;})});setEditId('week');}}><option value="6">6 месяцев</option><option value="9">9 месяцев</option></select></label><label>Дата начала<input type="date" value={draft.start} onChange={e=>updateDraft(shiftPlanStart(draft,e.target.value))}/></label></div><div className="builder-layout"><aside className="block-list"><div className="section-heading"><h2>Блоки сценария</h2><span>{draft.blocks.length}</span></div>{draft.blocks.map((b,i)=><button className={'block-item '+(draftBlock.id===b.id?'block-active':'')} key={b.id} onClick={()=>setEditId(b.id)}><span className="block-number">{i+1}</span><span><strong>{b.title}</strong><small>{dateLabel(b.date)}</small></span><CaretDown size={15} className="rotate-left"/></button>)}<button className="add-block" onClick={()=>setModal('add-block')}><Plus size={19}/>Добавить блок</button><div className="builder-hint"><Info size={18}/><p>Блоки приходят по датам. Выполнение чек-листов не влияет на расписание.</p></div></aside><section className="block-editor"><div className="editor-header"><div><p className="eyebrow">Настройка блока</p><h2>{draftBlock.title}</h2></div><div className="editor-tools"><button className="icon-button" aria-label="Поднять блок" disabled={draft.blocks[0].id===draftBlock.id} onClick={()=>moveBlock(-1)}><ArrowUp size={18}/></button><button className="icon-button" aria-label="Опустить блок" disabled={draft.blocks.at(-1)?.id===draftBlock.id} onClick={()=>moveBlock(1)}><ArrowDown size={18}/></button><button className="icon-button danger" aria-label="Удалить блок" disabled={draft.blocks.length===1} onClick={()=>{updateDraft({blocks:draft.blocks.filter(x=>x.id!==draftBlock.id)});setEditId(draft.blocks.find(x=>x.id!==draftBlock.id)!.id);}}><Trash size={18}/></button></div></div><div className="editor-fields"><label>Название блока<input value={draftBlock.title} onChange={e=>updateBlock({title:e.target.value})}/></label><label>Дата сообщения<input type="date" value={draftBlock.date} onChange={e=>updateBlock({date:e.target.value})}/></label></div><div className="tabs" role="tablist" aria-label="Получатель сообщения">{(['intern','buddy','lead'] as const).map(r=><button key={r} role="tab" aria-selected={editRole===r} onClick={()=>setEditRole(r)}>{r==='intern'?'Стажёру':r==='buddy'?'Бадди':'Руководителю'}</button>)}</div><label className="editor-copy">Сообщение {editRole==='intern'?'стажёру':editRole==='buddy'?'бадди':'руководителю'}<textarea rows={5} value={personalizeText(draftBlock[editRole],draft)} onChange={e=>updateBlock({[editRole]:e.target.value})}/></label>{editRole==='intern'&&<><div className="section-heading"><h3>Пункты чек-листа стажёра</h3><button className="text-link" onClick={()=>updateBlock({tasks:[...draftBlock.tasks,{id:crypto.randomUUID(),title:'Новый шаг',description:'',link:'Открыть материал',kind:'team'}]})}><Plus size={15}/>Добавить пункт</button></div><div className="editable-tasks">{draftBlock.tasks.map((t,i)=><div key={t.id} className="editable-task"><span>{i+1}</span><div><input aria-label={`Название пункта ${i+1}`} value={t.title} onChange={e=>updateBlock({tasks:draftBlock.tasks.map(x=>x.id===t.id?{...x,title:e.target.value}:x)})}/><input className="quiet-input" aria-label={`Описание пункта ${i+1}`} value={t.description} placeholder="Описание и помощь" onChange={e=>updateBlock({tasks:draftBlock.tasks.map(x=>x.id===t.id?{...x,description:e.target.value}:x)})}/><input type="url" className="quiet-input" aria-label={`Ссылка пункта ${i+1}`} value={t.url??''} placeholder="Рабочая ссылка · https://…" onChange={e=>updateBlock({tasks:draftBlock.tasks.map(x=>x.id===t.id?{...x,url:e.target.value}:x)})}/></div><button className="icon-button" aria-label={`Удалить пункт ${i+1}`} onClick={()=>updateBlock({tasks:draftBlock.tasks.filter(x=>x.id!==t.id)})}><Trash size={16}/></button></div>)}</div></>}{editRole!=='intern'&&HelperTaskEditor()}<button className="button preview-button" onClick={()=>setModal('preview')}><Monitor size={18}/>Предпросмотр всех ролей</button></section><aside className="builder-preview"><div className="section-heading"><h3>Так увидит участник</h3><Badge>Макет</Badge></div>{StageMessage({b:draftBlock,p:draft,asRole:editRole,preview:true})}<p className="muted small">Это предпросмотр. Публикация запускает сценарий по настроенным датам.</p></aside></div></div>;}

  function Knowledge(){return <div key="knowledge" className="knowledge-page"><div className="page-heading"><div><p className="eyebrow">Источники для ответов</p><h1>Материалы команды</h1><p>Проверенные инструкции, контакты и памятки для Хомыча.</p></div><button className="button button-primary" onClick={()=>{setSourceTitle('');setSourceDraft('');setSourceCategory('');setSourceError('');setEditingSourceId(null);setUploadName('');setModal('add-source');}}><Plus size={18}/>Добавить материал</button></div>{serverMode?<div className="proposal-banner"><Info size={20}/><div><strong>Источник ответов Хомыча</strong><p>Материалы команды плюс публичный Avito Playbook. Каждое сохранение — новая версия; пока она готовится к поиску, ответы используют прежнюю. Личный чат стажёра здесь не виден.</p></div></div>:<div className="proposal-banner"><Info size={20}/><div><strong>Вариант управления базой знаний</strong><p>Роль владельца и порядок обновления материалов ещё согласуются. Доступ к личному чату стажёра здесь не предоставляется.</p></div></div>}{mode==='source-error'&&<div className="error-banner"><WarningCircle size={20}/><div>Не удалось обновить материал. Для ответов остаётся прежняя версия.<button className="text-link" onClick={()=>{setMode('normal');notify('Демонстрационное обновление повторено');}}>Повторить обновление</button></div></div>}<div className="sources-toolbar"><label className="search-field"><MagnifyingGlass size={19}/><input aria-label="Поиск материалов" placeholder="Найти материал" value={sourceSearch} onChange={e=>setSourceSearch(e.target.value)}/></label><span className="muted small">{publicSources.filter(s=>s.status!=='excluded').length} материала · {plan.team}</span></div><div className="source-table"><div className="source-table-header"><span>Материал</span><span>Раздел</span><span>Состояние</span><span/></div>{publicSources.filter(s=>s.title.toLowerCase().includes(sourceSearch.toLowerCase())).map(s=><div className="source-row" key={s.id}><button className="source-title" onClick={()=>{setSourceId(s.id);setModal('source');}}><span className="document-icon"><FileText size={23}/></span><span><strong>{s.title}</strong><small>Обновлён: {s.updated}</small></span></button><span className="source-category">{s.category}</span><Badge tone={s.status==='ready'?'green':s.status==='indexing'?'blue':'red'}>{s.status==='ready'?'В поиске':s.status==='indexing'?'Обновляется':serverMode?'Не обновился':'Ошибка'}</Badge><button className="icon-button" aria-label={'Исключить '+s.title} onClick={()=>void excludeSource(s.id)}><Trash size={18}/></button></div>)}</div>{!publicSources.filter(s=>s.title.toLowerCase().includes(sourceSearch.toLowerCase())).length&&<Empty title="Материалы не найдены" text="Попробуй изменить запрос или добавь новую памятку." icon={<MagnifyingGlass size={34}/>}/>}<div className="knowledge-note"><LockSimple size={18}/><p>Для ответа используются только материалы, доступные стажёру. При нехватке данных бот предлагает уточнить вопрос или обратиться к человеку.</p></div></div>;}

  function Components(){return <div key="knowledge" className="knowledge-page"><div className="page-heading"><div><p className="eyebrow">Визуальная основа макета</p><h1>Компоненты интерфейса</h1><p>Шрифт Onest, палитра и компоненты команды Origination.</p></div></div><div className="component-grid"><section><h2>Кнопки</h2><div className="sample-row"><button className="button button-primary" onClick={()=>notify('Основное действие')}>Продолжить</button><button className="button" onClick={()=>notify('Вторичное действие')}>Посмотреть</button><button className="button" disabled>Недоступно</button></div></section><section><h2>Поля</h2><label>Название<input placeholder="Например, знакомство с командой"/></label><label>Поле с ошибкой<input className="input-error" defaultValue="" aria-invalid="true" placeholder="Обязательное поле"/><small className="error-text">Укажи название</small></label></section><section><h2>Состояния</h2><div className="sample-row"><Badge tone="blue">По расписанию</Badge><Badge tone="green">Сохранено</Badge><Badge tone="red">Не отправлено</Badge><Badge>Предпросмотр</Badge></div><Spinner label="Обновляем материалы"/></section><section><h2>Пункты и сообщения</h2><label className="sample-checkbox"><input type="checkbox"/>Познакомиться с командой</label><label className="sample-checkbox"><input type="checkbox" defaultChecked/>Найти основные каналы</label><div className="checklist-note"><Info size={16}/>Отметки не влияют на следующий период</div></section></div><div className="design-source-note"><BookOpen size={20}/><p>Источник: <a href="https://www.figma.com/design/pDmYotOu0D7yKMmhoN9UtG/Avito?node-id=10-1289" target="_blank" rel="noreferrer">дизайн-система команды Origination</a>. Типографика Onest, цвета, плоские компоненты и маскот взяты из вашего макета. Согласованная логика бота сохранена.</p></div></div>;}

  const sourceModals:Record<string,{title:string;body:string;bullets?:string[]}>= {
    team:{title:'Команда и каналы',body:'Познакомься с людьми, с которыми будешь работать. Все имена и организационные сведения демонстрационные.',bullets:['Бадди помогает с инструментами, задачами и знакомством.','Руководитель обсуждает цели и важные решения.','Ссылки на внутренние чаты добавляет команда.']},
    access:{title:'Получение рабочих доступов',body:'Пройди этот шаг вместе с бадди. Бот даёт инструкцию, а действие выполняется в согласованном сервисе.',bullets:['Уточни у Даши список нужных ресурсов.','Открой корпоративную форму заявки по ссылке команды.','Укажи ресурс и обоснование. Если есть вопросы, вернись к бадди.']},
    task:{title:'Подготовка к первой задаче',body:'Руководитель определяет постановку и критерии. Бадди помогает разобраться с инструментами и первыми шагами.',bullets:['Прочитай постановку в рабочем инструменте команды.','Проверь, что понятны ожидаемый результат и критерии.','Обсуди с бадди вопросы и недостающие доступы.']},
    courses:{title:'Вводные курсы и материалы',body:'Список курсов, сроки и рабочие ссылки передаёт HR. Корпоративная система обучения в демо не подключена.',bullets:['Вводные материалы о компании.','Обязательные курсы по защите данных.','Командные инструкции по инструментам.']},
    practice:{title:'Памятка по учебной практике',body:'Если практика нужна для учебного заведения, уточни у HR список документов и сроки. Если не нужна, шаг можно пропустить.',bullets:['Собери требования учебного заведения.','Заранее обсуди сроки подготовки документов.','Согласуй действия с HR вне бота.']},
    midpoint:{title:'Подготовка к экватору',body:'Результаты и выводы готовятся вне продукта. Бот помогает вовремя вспомнить о подготовке.',bullets:['Собери примеры выполненных задач.','Запиши свой вклад, трудности и вопросы.','Обсуди материалы с бадди и договорись о встрече с руководителем.']},
    final:{title:'Подготовка итогового разговора',body:'Уточни у руководителя формат и критерии. Материалы подготовь в привычных инструментах команды.',bullets:['Выбери несколько понятных примеров работы.','Расскажи, что получилось и чему научился.','Обсуди дальнейший шаг или срок ответа с руководителем.']},
    handoff:{title:'Передача задач команде',body:'Передай открытые задачи и контекст вместе с бадди. Формальная отметка в боте не заменяет реальный приём работы.',bullets:['Укажи статус каждой задачи.','Передай рабочие ссылки и важные договорённости.','Убедись, что у каждой задачи есть получатель.']},
  };

  if(API_ENABLED&&!session)return sessionChecked?<Login onLogin={startSession}/>:<main className="prototype-root login-page"><Spinner label="Проверяем вход…"/></main>;
  return <main className={'prototype-root '+(mobile?'mobile-preview ':'')+(theme==='dark'?'dark-theme':'')}>
    {!serverMode&&<div className="review-bar"><span className="review-label"><span/>Интерактивный макет</span>{API_ENABLED&&session?<div className="review-role review-user"><span>Вход: {session.subject.displayName} · {roleNames[roleOf(session)]}</span><button className="review-state" onClick={()=>void logout()}><SignOut size={16}/>Выйти</button></div>:<div className="review-role"><span>Смотреть как</span><select aria-label="Роль для просмотра" value={role} onChange={e=>changeRole(e.target.value as Role)}>{(['intern','buddy','lead','hr'] as Role[]).map(r=><option value={r} key={r}>{roleNames[r]}{r==='hr'?' · вариант':''}</option>)}</select></div>}<div className="review-role review-intern"><select aria-label="Стажёр для просмотра" value={viewIntern.id} onChange={e=>selectIntern(e.target.value)}>{state.interns.map(i=><option value={i.id} key={i.id}>{i.name}</option>)}</select></div><div className="review-tools"><span>Демоданные</span><button className="icon-button" aria-label={mobile?'Открыть широкий вид':'Открыть мобильный вид'} onClick={()=>setMobile(!mobile)}>{mobile?<Monitor size={18}/>:<DeviceMobile size={18}/>}</button><button className="review-state" onClick={()=>setModal('states')}><SlidersHorizontal size={16}/>Состояния</button></div></div>}
    <div className="app-shell"><header className="app-header"><button className="brand" onClick={()=>setPage('messages')} aria-label="Хомыч — сообщения"><Avatar bot size={36}/><span>Хомыч</span><span className="brand-caption">Стажировка в Авито</span></button><nav className="main-nav" aria-label="Разделы"><button className={page==='messages'?'active':''} onClick={()=>setPage('messages')}><ChatCircle size={18}/>Сообщения</button>{role!=='hr'&&<button className={page==='plan'?'active':''} onClick={()=>setPage('plan')}><ListChecks size={18}/>План</button>}{role==='lead'&&<button className={page==='builder'||page==='scenarios'?'active':''} onClick={()=>setPage('scenarios')}><ClipboardText size={18}/>Сценарий</button>}{(role==='lead'||role==='hr')&&<button className={page==='knowledge'?'active':''} onClick={()=>setPage('knowledge')}><BookOpen size={18}/>Материалы</button>}</nav><div className="header-actions"><button className="icon-button notification-trigger" aria-label={unreadNotes?`Открыть уведомления, новых: ${unreadNotes}`:'Открыть уведомления'} onClick={openNotifications}><Bell size={22}/>{(!serverMode||unreadNotes>0)&&<span/>}</button><button className="profile-button" aria-label="Открыть профиль" onClick={()=>setModal('profile')}><Avatar size={32}/><span>{serverMode?session!.subject.displayName.split(' ')[0]:role==='intern'?plan.intern.split(' ')[0]:role==='buddy'?plan.buddy.split(' ')[0]:role==='lead'?plan.lead.split(' ')[0]:'Анна'}</span><CaretDown size={13}/></button></div></header>
    {saveError&&<div role="alert" className="error-banner persistence-error"><WarningCircle size={20}/><span>{saveError}</span><button className="button" onClick={()=>restoreBlocked.current?window.location.reload():saveCurrent(true)}>Повторить</button></div>}{page==='messages'?Messages():page==='plan'?PlanPage():page==='scenarios'?Scenarios():page==='builder'?(selectedScenario?Builder():Scenarios()):page==='knowledge'?Knowledge():Components()}</div>
    {toast&&<div className="toast" role="status">{toastError?<WarningCircle size={20} weight="fill"/>:<CheckCircle size={20} weight="fill"/>}{toast}</div>}
    {modal==='contact'&&<Modal title={roleNames[contactRole]} onClose={()=>setModal(null)}><div className="contact-card"><Avatar size={64}/><h3>{contactRole==='intern'?(contactPlan??plan).intern:contactRole==='buddy'?(contactPlan??plan).buddy:contactRole==='lead'?(contactPlan??plan).lead:people.hr}</h3><p>{contactRole==='intern'?'Участник стажировки. Обсудите совместные шаги.':contactRole==='buddy'?'Поможет с задачами, инструментами и адаптацией':contactRole==='lead'?'Обсудит цели, результаты и важные решения':'Подскажет по документам и организационным вопросам'}</p><Badge>Демонстрационный контакт</Badge></div><p className="muted small">Рабочая ссылка на контакт появится после подключения каналов команды.</p></Modal>}
    {modal==='team-panel'&&<Modal title="Твоя команда" onClose={()=>setModal(null)}><div className="chat-contacts">{(['buddy','lead','hr'] as Role[]).map(r=><button className="person-row" key={r} onClick={()=>openLink(r)}><Avatar size={38}/><span><strong>{r==='buddy'?plan.buddy:r==='lead'?plan.lead:people.hr}</strong><small>{roleNames[r]}</small></span><ArrowRight size={17}/></button>)}</div><p className="muted small">{serverMode?'Контакты демонстрационные.':'Все контакты в макете демонстрационные.'}</p></Modal>}
    {modal&&sourceModals[modal]&&<Modal title={sourceModals[modal].title} onClose={()=>setModal(null)} footer={<><button className="button" onClick={()=>openLink('buddy')}><Users size={17}/>Связаться с бадди</button><button className="button button-primary" onClick={()=>setModal('external')}>Открыть рабочую ссылку<ArrowRight size={17}/></button></>}><p>{sourceModals[modal].body}</p><ol className="instruction-list">{sourceModals[modal].bullets?.map(t=><li key={t}>{t}</li>)}</ol><div className="inline-note"><Info size={18}/>Это демонстрационная памятка. Реальные материалы добавит команда.</div>{modal==='access'&&serverMode&&(access.operationId?<button className="text-link conditional-link" onClick={()=>setModal('access-status')}>Статус заявки{access.number?' '+access.number:''}</button>:role==='intern'?<button className="button button-primary conditional-link" onClick={()=>openAccess(true)}>Подать заявку на доступ</button>:<p className="muted small">Заявку подаёт стажёр. Здесь появится её статус.</p>)}{modal==='access'&&!serverMode&&directDemo&&<button className="text-link conditional-link" onClick={()=>openAccess(true)}>Открыть условную интеграцию заявки</button>}{modal==='task'&&(directDemo||serverMode)&&(access.status!=='approved'||(serverMode&&!protectedTask)?<div className="error-banner"><LockSimple size={18}/><div>Защищённая постановка закрыта до одобрения нужного доступа. Это не влияет на другие периоды.{(!serverMode||access.operationId||role==='intern')&&<button className="text-link" onClick={()=>openAccess()}>{access.operationId?'Проверить доступ':'Подать заявку на доступ'}</button>}</div></div>:<div className="protected-task"><Badge tone="green">Нужный доступ одобрен</Badge><h3>{protectedTask?.title??'Первая задача'}</h3><p>{protectedTask?.description??'Демонстрационная постановка: добавь проверку входных параметров в учебный сервис команды.'}</p><p className="muted small">{serverMode?'Постановка пришла с сервера: доступ проверен по одобренной заявке. Обсуди план работы с бадди.':'Критерии: корректные параметры принимаются, некорректные возвращают понятную ошибку. Обсуди план работы с бадди.'}</p></div>)}</Modal>}
    {modal==='external'&&<Modal title="Переход в рабочий инструмент" onClose={()=>setModal(null)}><div className="empty-state compact"><LinkSimple size={38}/><h3>Корпоративная ссылка пока не подключена</h3><p>{serverMode?'Здесь откроется адрес из блока сценария. В демо-сценарии рабочие ссылки не заданы.':'В рабочем продукте здесь откроется адрес из блока сценария. В макете внешние системы не вызываются.'}</p><a className="button" href="https://github.com/avito-tech/playbook" target="_blank" rel="noreferrer">Открыть публичный AvitoTech Playbook<ArrowRight size={16}/></a></div></Modal>}
    {modal==='source'&&activeSource&&<Modal title={activeSource.title} onClose={()=>setModal(null)} footer={page==='knowledge'?<button className="button" onClick={()=>{setEditingSourceId(activeSource.id);setSourceTitle(activeSource.title);setSourceDraft(activeSource.body);setSourceCategory(activeSource.category);setSourceError('');setUploadName('');setModal('add-source');}}>Редактировать материал</button>:undefined}>{serverMode?<Badge tone={activeSource.status==='ready'?'green':activeSource.status==='indexing'?'blue':'red'}>{activeSource.status==='ready'?'В поиске':activeSource.status==='indexing'?'Обновляется':activeSource.status==='excluded'?'Исключён':'Ошибка обновления'}</Badge>:<Badge tone="blue">Демонстрационный материал</Badge>}<p className="source-body">{activeSource.body}</p><div className="inline-note"><BookOpen size={18}/>Материал команды · {activeSource.category}</div></Modal>}
    {modal==='settings'&&<Modal title="Настройки уведомлений" onClose={()=>setModal(null)}><label className="setting-row"><span><strong>Звук новых сообщений</strong><small>{serverMode?'Пока вкладка Хомыча открыта':'Воспроизводится в открытом макете'}</small></span><input type="checkbox" role="switch" checked={sound} onChange={e=>setSound(e.target.checked)}/></label><button className="button" disabled={!sound} onClick={()=>{beep();notify('Проверка звука');}}>Проверить звук</button><div className="inline-note"><Info size={18}/>Фоновая доставка и системные разрешения зависят от выбранной среды бота.</div></Modal>}
    {modal==='notifications'&&serverMode&&<Modal title="Уведомления" onClose={()=>setModal(null)}>{notes.length?<div className="notification-list">{notes.map(n=>{const fresh=n.createdAt>notesBefore.current;return <button key={n.id} className={'notification-item notification-button'+(fresh?' notification-new':'')} onClick={()=>openNote(n)}><span className="notification-icon"><CalendarBlank size={23}/></span><div><strong>{n.title.replace(/^Хомыч:\s*/,'')}</strong><small>{new Date(n.createdAt).toLocaleString('ru-RU',{day:'numeric',month:'long',hour:'2-digit',minute:'2-digit'})}{fresh?' · новое':''}</small></div><ArrowRight size={16}/></button>;})}</div>:<p className="muted">Пока пусто. Хомыч напишет, когда наступит первый этап.</p>}<p className="muted small">Сейчас уведомления приходят в веб-интерфейс. Пуш и мессенджер — открытый вопрос OQ02.</p></Modal>}
    {modal==='notifications'&&!serverMode&&<Modal title="Уведомления" onClose={()=>setModal(null)}>{mode==='delivery-error'&&<div className="error-banner"><WarningCircle size={20}/><div>Сообщение пока не доставлено. Остальные получатели продолжают получать свои напоминания.<button className="text-link" onClick={()=>{setMode('normal');notify('Демонстрационная доставка повторена');}}>Повторить доставку</button></div></div>}<div className="notification-item"><span className="notification-icon"><CalendarBlank size={23}/></span><div><strong>{block.title}</strong><p>{role==='intern'?'Твой чек-лист и материалы уже в переписке.':`Пора помочь стажёру: ${plan.intern}.`}</p><small>{dateLabel(block.date)} · 09:00</small><button className="text-link" onClick={()=>{setPage('messages');setModal(null);}}>Перейти к сообщению<ArrowRight size={15}/></button></div></div><div className="notification-item"><span className="notification-icon"><Users size={23}/></span><div><strong>Команда рядом</strong><p>Бадди и руководитель получили отдельные сообщения о своей помощи.</p><small>По сценарию стажировки</small></div></div><div className="inline-note">Все уведомления в этом макете демонстрационные.</div></Modal>}
    {modal==='profile'&&serverMode&&ProfileModal()}
    {modal==='profile'&&!serverMode&&<Modal title="Просмотр макета" onClose={()=>setModal(null)}><p className="muted">Переключение роли используется для проверки экранов. Это не механизм авторизации рабочего продукта.</p>{(['intern','buddy','lead','hr'] as Role[]).map(r=><button className="profile-role-row" key={r} onClick={()=>{changeRole(r);setModal(null);}}><Avatar size={32}/><span>{roleNames[r]}{r==='hr'?' · вариант':''}</span>{role===r&&<Check size={19}/>}</button>)}<button className="text-link" onClick={()=>{setPage('components');setModal(null);}}><SquaresFour size={18}/>Посмотреть компоненты</button></Modal>}
    {modal==='states'&&serverMode&&<Modal title="Демо-стенд" onClose={()=>setModal(null)}><p className="muted">Инструменты для показа. В рабочей версии этого раздела нет: дата идёт сама, заявки решает настоящий сервис.</p>
      <div className="demo-clock"><label>Дата на стенде<input aria-label="Дата на стенде" type="date" key={today} ref={clockInput} defaultValue={today}/></label><p className="muted small">Хомыч сразу пришлёт этапы, которые наступили к этой дате. Дата сохраняется на сервере.</p><div className="button-row"><button className="button button-primary" onClick={()=>{const date=clockInput.current?.value;if(date)void setServerClock(date);}}>Применить дату</button><button className="button" onClick={()=>void setServerClock(null)}>Вернуть сегодняшнюю</button></div></div>
      <label>Ответ сервиса доступов на новые заявки<select value={accessOutcome} onChange={e=>void changeAccessOutcome(e.target.value as typeof accessOutcome)}><option value="approved">Одобрение</option><option value="denied">Отказ</option><option value="unknown">Ответ потерян (таймаут)</option></select></label>
      <div className="state-actions"><button className="button" onClick={()=>setModal('demo-reset')}>Начать демо заново</button></div>
    </Modal>}
    {modal==='demo-reset'&&<Modal title="Начать демо заново?" onClose={()=>setModal('states')} footer={<><button className="button" onClick={()=>setModal('states')}>Отмена</button><button className="button button-primary" onClick={()=>void resetDemo()}>Сбросить демо</button></>}><p>Сервер удалит сообщения, отметки, заявки на доступ и созданные сценарии, вернёт сегодняшнюю дату и три демо-сценария. Пользователи и назначения останутся.</p></Modal>}
    {modal==='states'&&!serverMode&&<Modal title="Состояния прототипа" onClose={()=>setModal(null)}><p className="muted">{serverMode?'Данные хранятся на сервере. Здесь можно сдвинуть дату и выбрать ответ сервиса доступов.':'Переключатели для просмотра макетов. Реальные сервисы не вызываются.'}</p><div className="demo-clock"><label>Демонстрационная дата<input aria-label="Демонстрационная дата" type="date" key={state.demoDate??'realtime'} ref={clockInput} defaultValue={today}/></label><p className="muted small">Только проверка макета. Бот сам добавит сообщения, срок которых наступил. В пользовательском чате выбора этапов нет.</p><div className="button-row"><button className="button button-primary" onClick={()=>{const date=clockInput.current?.value;if(!date)return;if(serverMode)void setServerClock(date);else setState(s=>({...s,demoDate:date}));}}>Применить дату</button><button className="button" onClick={()=>{if(serverMode)void setServerClock(null);else setState(s=>({...s,demoDate:null}));}}>Использовать сегодняшнюю дату</button></div></div><label>Тема интерфейса<select value={theme} onChange={e=>setTheme(e.target.value)}><option value="light">Светлая</option><option value="dark">Тёмная</option></select></label><label>Состояние переписки<select value={mode} onChange={e=>setMode(e.target.value)}><option value="normal">Обычный сценарий</option><option value="no-plan">Нет опубликованного плана</option><option value="network-error">Ошибка загрузки</option><option value="model-error">Модель не отвечает</option><option value="no-source">Нет подходящего источника</option><option value="delivery-error">Сбой доставки уведомления</option><option value="source-error">Ошибка обновления материала</option></select></label>{!serverMode&&<label className="setting-row"><span><strong>Условная интеграция доступов</strong><small>Отдельная демонстрация · вопрос OQ08</small></span><input type="checkbox" checked={directDemo} onChange={e=>setDirectDemo(e.target.checked)}/></label>}{(directDemo||serverMode)&&<><label>{serverMode?'Ответ сервиса доступов на новые заявки':'Ответ демонстрационного сервиса'}<select value={accessOutcome} onChange={e=>void changeAccessOutcome(e.target.value as typeof accessOutcome)}><option value="approved">Одобрение</option><option value="denied">Отказ</option><option value="unknown">{serverMode?'Ответ потерян (таймаут)':'Неизвестный результат'}</option></select></label><button className="button" onClick={()=>openAccess()}>Открыть заявку</button></>}<div className="state-actions"><button className="button" onClick={()=>{setPage('components');setModal(null);}}>Компоненты</button><button className="button" onClick={()=>{localStorage.removeItem(STORE);localStorage.removeItem(LEGACY_STORE);setState(initialState());setViewInternId('intern-sasha');setSelectedScenarioId('scenario-original');restoreBlocked.current=false;setSaveError('');if(chatScroll.current)chatScroll.current.scrollTop=0;setAccess(EMPTY_ACCESS);setDirectDemo(false);setMode('normal');setRole('intern');setPage('messages');setModal(null);notify('Демонстрация начата заново');}}>Начать демонстрацию заново</button></div></Modal>}
    {modal==='publish'&&<Modal title="Опубликовать сценарий?" onClose={()=>setModal(null)} footer={<><button className="button" onClick={()=>setModal(null)}>Вернуться к настройке</button><button className="button button-primary" onClick={publish}>Опубликовать</button></>}><p>Стажёр, бадди и руководитель будут получать предусмотренные сообщения по датам этого сценария.</p>{selectedScenario?.internId&&state.activeScenarios[selectedScenario.internId]&&state.activeScenarios[selectedScenario.internId].scenarioId!==selectedScenario.id&&<div className="inline-note replacement-note"><Info size={20}/><p>Этот вариант заменит «{state.scenarios.find(s=>s.id===state.activeScenarios[selectedScenario.internId!].scenarioId)?.published?.name}» у стажёра {draft.intern}. Прежний сценарий останется в списке. Уже наступившие этапы повторно не отправляются; новый вариант используется для дат после {dateLabel(today,true)}</p></div>}<div className="publish-summary"><p><span>Стажёр</span><strong>{draft.intern}</strong></p><p><span>Бадди</span><strong>{draft.buddy}</strong></p><p><span>Длительность</span><strong>{draft.months} месяцев</strong></p><p><span>Блоков</span><strong>{draft.blocks.length}</strong></p></div><p className="muted small">{serverMode?'Сценарий публикуется на сервере, сообщения пойдут по датам.':'В макете публикация сохраняет сценарий только в этом браузере.'} Уже показанные сообщения остаются в истории.</p></Modal>}
    {modal==='preview'&&<Modal title="Предпросмотр сообщений" onClose={()=>setModal(null)} wide><div className="preview-role-tabs tabs" role="tablist">{(['intern','buddy','lead'] as Role[]).map(r=><button role="tab" aria-selected={previewRole===r} key={r} onClick={()=>setPreviewRole(r)}>{roleNames[r]}</button>)}</div><div className="preview-conversation">{StageMessage({b:draftBlock,p:draft,asRole:previewRole,preview:true})}</div></Modal>}
    {modal==='add-block'&&<Modal title="Готовые блоки" onClose={()=>setModal(null)}><p className="muted">Добавь блок и настрой даты и тексты для участников.</p><div className="library-list">{makePlan(draft.months).blocks.map(b=><button key={b.id} onClick={()=>{const added={...clone(b),id:b.id+'-'+crypto.randomUUID()};updateDraft({blocks:[...draft.blocks,added]});setEditId(added.id);setModal(null);notify('Блок добавлен в черновик');}}><CalendarBlank size={22}/><span><strong>{b.title}</strong><small>{b.tasks.length} пункта · сообщения трём ролям</small></span><Plus size={20}/></button>)}</div></Modal>}
    {modal==='add-source'&&<Modal title={editingSourceId?'Обновить материал':'Добавить материал'} onClose={()=>setModal(null)} footer={<><button className="button" onClick={()=>setModal(null)}>Отмена</button><button className="button button-primary" disabled={sourceBusy} onClick={saveSource}>{sourceBusy?'Сохраняем…':editingSourceId?'Сохранить изменения':'Добавить материал'}</button></>}><label>Название<input value={sourceTitle} onChange={e=>setSourceTitle(e.target.value)} placeholder="Например, инструкция по инструментам"/></label>{serverMode&&<label>Раздел<input value={sourceCategory} onChange={e=>setSourceCategory(e.target.value)} placeholder="Например, Инструменты"/></label>}<label>Текст материала<textarea rows={7} value={sourceDraft} onChange={e=>setSourceDraft(e.target.value)} placeholder="Добавь проверенную инструкцию или памятку"/></label><input ref={fileInput} type="file" accept=".txt,.md" className="visually-hidden" onChange={e=>void readFile(e.target.files?.[0])}/><button className="button" onClick={()=>fileInput.current?.click()}><CloudArrowUp size={18}/>{uploadName||'Выбрать TXT или Markdown'}</button>{sourceError&&<p role="alert" className="error-text">{sourceError}</p>}<div className="inline-note"><LockSimple size={16}/>{serverMode?`Материал увидят в ответах стажёры команды «${plan.team}». Новая версия попадёт в поиск через несколько секунд, до этого ответы используют прежнюю.`:`Материал предназначен для команды «${plan.team}». В макете поиск имитируется.`}</div></Modal>}
    {modal==='access-form'&&<Modal title="Заявка на доступ" onClose={()=>setModal(null)} footer={<><button className="button" onClick={()=>setModal(null)}>Отмена</button><button className="button button-primary" disabled={!reason.trim()||accessBusy} onClick={()=>{if(serverMode){void sendAccessRequest();return;}setAccess(a=>submitAccess(a,reason));setModal('access-status');notify('Демонстрационная заявка сохранена');}}>{accessBusy?'Отправляем…':'Подтвердить отправку'}</button></>}><Badge>{serverMode?'Сервис доступов · демо-стенд':'Условная демонстрация'}</Badge><p>Проверь ресурс и обоснование перед подтверждением.</p><label>Ресурс<input value={serverMode?ACCESS_RESOURCE.title:access.resource} readOnly/></label><label>Обоснование<textarea rows={4} value={reason} onChange={e=>{setReason(e.target.value);accessKeys.current=null;}}/></label><p className="muted small">{serverMode?'Заявка уйдёт в сервис доступов только после твоего подтверждения. Повторное нажатие не создаст вторую заявку.':'Внешний сервис не вызывается. Повторное подтверждение использует ту же демонстрационную операцию.'}</p></Modal>}
    {modal==='access-status'&&<Modal title="Статус заявки" onClose={()=>setModal(null)} footer={<button className="button button-primary" onClick={()=>{if(serverMode){void checkAccessStatus();return;}setAccess(a=>({...a,status:accessOutcome}));notify(accessOutcome==='unknown'?'Результат пока неизвестен. Операция сохранена.':'Демонстрационный статус обновлён');}}>Проверить статус</button>}><Badge>{serverMode?'Сервис доступов · демо-стенд':'Условная демонстрация'}</Badge><div className="access-status"><span className="status-icon">{access.status==='approved'?<CheckCircle size={44}/>:access.status==='denied'?<WarningCircle size={44}/>:<Clock size={44}/>}</span><h3>{access.status==='approved'?'Доступ одобрен':access.status==='denied'?'В доступе отказано':access.status==='unknown'?'Результат пока неизвестен':'Заявка на согласовании'}</h3><p>{access.number?`${access.number} · `:''}{access.resource}</p><p className="muted">{access.status==='denied'?'Обсуди причину с бадди.':access.status==='unknown'?'Ответ сервиса не дошёл. Операция сохранена — проверка сверит её по номеру операции, вторая заявка не создаётся.':access.status==='approved'&&serverMode?'Первая задача открыта в сообщении «Подготовка к первой задаче».':'Остальные периоды и сообщения доступны независимо от этого статуса.'}</p></div><button className="text-link" onClick={()=>openLink('buddy')}>Контакт бадди</button></Modal>}
  </main>;
}
