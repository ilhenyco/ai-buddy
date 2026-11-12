import { makePlan, shiftPlanStart, seedSources, people, validExternalUrl, type Block, type ChatMessage, type Plan, type Source } from './domain.ts';
import type {QuestionSelection} from './suggestions.ts';

export const STORE = 'homych.frontend.bot.v3';
export const LEGACY_STORE = 'homych.frontend.bot.v2';
export const demoInterns = [
  {id:'intern-sasha',name:people.intern},
  {id:'intern-masha',name:'Маша Петрова'},
  {id:'intern-ilya',name:'Илья Смирнов'},
];
export type Scenario = {id:string;internId:string|null;draft:Plan;published?:Plan;updatedAt:string};
export type Snapshot = {scenarioId:string;block:Block;plan:Plan;next?:Block};
export type FeedEntry = {kind:'checkpoint';id:string}|{kind:'chat';message:ChatMessage};
export type Runtime = {checks:Record<string,boolean>;feed:FeedEntry[];snapshots:Record<string,Snapshot>;suggestions?:QuestionSelection};
export type ActiveScenario = {scenarioId:string;afterDate:string|null};
export type AppState = {
  schema:3;interns:{id:string;name:string}[];scenarios:Scenario[];
  activeScenarios:Record<string,ActiveScenario>;runtimes:Record<string,Runtime>;
  sources:Source[];demoDate:string|null;
};
export type ScenarioFilter = 'all'|'drafts'|'published';
export const clone = <T,>(value:T):T => structuredClone(value);
export const emptyRuntime = ():Runtime => ({checks:{},feed:[],snapshots:{}});
export const eventId = (scenarioId:string,blockId:string) => `${scenarioId}:${blockId}`;
export const checkId = (role:string,event:string,task:string) => `${role}:${event}:${task}`;

export function initialState(now=new Date().toISOString()):AppState {
  const plan=makePlan();
  return {schema:3,interns:clone(demoInterns),scenarios:[{id:'scenario-original',internId:demoInterns[0].id,draft:clone(plan),published:plan,updatedAt:now}],
    activeScenarios:{[demoInterns[0].id]:{scenarioId:'scenario-original',afterDate:null}},
    runtimes:{[demoInterns[0].id]:emptyRuntime()},sources:clone(seedSources),demoDate:null};
}

/** Migration retains original v2 storage; checkpoint and checkbox references move together. */
export function migrateLegacy(old:any,now=new Date().toISOString()):AppState {
  if(!old?.plan?.blocks?.length||!old?.draft?.blocks?.length||!Array.isArray(old.feed)||!old.snapshots||!old.checks||!Array.isArray(old.sources))throw new Error('Не удалось прочитать прежние данные.');
  const state=initialState(now), scenario=state.scenarios[0], internId=scenario.internId!;
  scenario.published=clone(old.plan);scenario.draft=clone(old.draft);
  state.interns[0].name=old.plan.intern;
  const runtime=state.runtimes[internId];
  for(const [id,snapshot] of Object.entries(old.snapshots))runtime.snapshots[eventId(scenario.id,id)]={...clone(snapshot as Omit<Snapshot,'scenarioId'>),scenarioId:scenario.id};
  runtime.feed=old.feed.map((entry:FeedEntry)=>entry.kind==='checkpoint'?{...entry,id:eventId(scenario.id,entry.id)}:clone(entry));
  // Prefix instead of splitting: custom block IDs may contain delimiters.
  for(const [key,value] of Object.entries(old.checks)) {
    const colon=key.indexOf(':');
    runtime.checks[key.slice(0,colon+1)+scenario.id+':'+key.slice(colon+1)]=value as boolean;
  }
  state.sources=clone(old.sources);state.demoDate=old.demoDate??null;
  return state;
}

function validState(value:any):value is AppState {
  return value?.schema===3&&Array.isArray(value.interns)&&value.interns.length>0&&Array.isArray(value.scenarios)&&
    value.scenarios.every((s:any)=>typeof s.id==='string'&&Array.isArray(s.draft?.blocks)&&s.draft.blocks.length>0)&&
    !!value.activeScenarios&&!!value.runtimes&&Array.isArray(value.sources);
}
export function restoreState(storage:Pick<Storage,'getItem'>):{state:AppState;error:string} {
  try {
    const current=storage.getItem(STORE);
    if(current){const parsed=JSON.parse(current);if(!validState(parsed))throw new Error();return {state:parsed,error:''};}
    const legacy=storage.getItem(LEGACY_STORE);
    return {state:legacy?migrateLegacy(JSON.parse(legacy)):initialState(),error:''};
  } catch {return {state:initialState(),error:'Не удалось восстановить сохранённые данные. Исходная запись не изменена. Автосохранение приостановлено.'};}
}
export function persistState(storage:Pick<Storage,'setItem'>,state:AppState):string {
  try{storage.setItem(STORE,JSON.stringify(state));return '';}catch{return 'Не удалось сохранить изменения в браузере. Не закрывай вкладку и попробуй ещё раз.';}
}
export function latestState(storage:Pick<Storage,'getItem'>,current:AppState):AppState {
  const disk=restoreState(storage);
  return disk.error||JSON.stringify(disk.state)===JSON.stringify(current)?current:disk.state;
}
export function hasDraft(scenario:Scenario):boolean {
  return !scenario.published||JSON.stringify(scenario.draft)!==JSON.stringify(scenario.published);
}
export function listScenarios(state:AppState,query:string,filter:ScenarioFilter):Scenario[] {
  const needle=query.trim().toLocaleLowerCase('ru');
  return state.scenarios.filter(s=>(filter==='all'||(filter==='drafts'?hasDraft(s):!!s.published))&&
    `${s.draft.name} ${s.draft.intern}`.toLocaleLowerCase('ru').includes(needle)).sort((a,b)=>b.updatedAt.localeCompare(a.updatedAt));
}
export function addScenario(state:AppState,id:string,start:string,now=new Date().toISOString()):AppState {
  const draft=shiftPlanStart(makePlan(6),start);draft.name='Новый сценарий';draft.intern='';draft.version=0;
  return {...state,scenarios:[...state.scenarios,{id,internId:null,draft,updatedAt:now}]};
}
export function patchScenario(state:AppState,id:string,patch:Partial<Plan>,now=new Date().toISOString()):AppState {
  return {...state,scenarios:state.scenarios.map(s=>{
    if(s.id!==id)return s;
    // Published variants keep their owner so their history cannot move to another person.
    const draft={...s.draft,...patch,intern:s.published?s.published.intern:(patch.intern??s.draft.intern)};
    const internId=state.interns.find(i=>i.name===draft.intern)?.id??null;
    return {...s,draft,internId,updatedAt:now};
  })};
}
export function publicationError(scenario:Scenario|undefined):string {
  if(!scenario)return 'Сценарий не найден.';
  const p=scenario.draft;
  if(!p.name.trim())return 'Укажи название сценария.';
  if(!scenario.internId||!p.intern||!p.buddy||!p.lead)return 'Выбери стажёра, бадди и руководителя.';
  if(!p.start||!p.blocks.length||p.blocks.some(b=>!b.title.trim()||!b.date||!b.intern.trim()||b.tasks.some(t=>!t.title.trim()||(!!t.url&&!validExternalUrl(t.url)))))return 'Проверь даты, названия блоков, сообщения, пункты и ссылки (http или https).';
  return '';
}
export function publishScenario(state:AppState,id:string,today:string,now=new Date().toISOString()):AppState {
  const scenario=state.scenarios.find(s=>s.id===id);const error=publicationError(scenario);
  if(error)throw new Error(error);
  const current=scenario!;const internId=current.internId!;const prior=state.activeScenarios[internId];
  const published=clone({...current.draft,version:(current.published?.version??0)+1});
  const afterDate=prior&&prior.scenarioId!==id?today:prior?.afterDate??null;
  return {...state,scenarios:state.scenarios.map(s=>s.id===id?{...s,published,draft:clone(published),updatedAt:now}:s),
    activeScenarios:{...state.activeScenarios,[internId]:{scenarioId:id,afterDate}},
    runtimes:{...state.runtimes,[internId]:state.runtimes[internId]??emptyRuntime()}};
}
export function updateRuntime(state:AppState,internId:string,update:(runtime:Runtime)=>Runtime):AppState {
  return {...state,runtimes:{...state.runtimes,[internId]:update(state.runtimes[internId]??emptyRuntime())}};
}
/** Deliver only published active variants; replacement never backfills elapsed dates. */
export function deliverDue(state:AppState,today:string):AppState {
  let result=state;
  for(const [internId,active] of Object.entries(state.activeScenarios)){
    const scenario=state.scenarios.find(s=>s.id===active.scenarioId&&s.internId===internId);const plan=scenario?.published;
    if(!plan)continue;
    const runtime=result.runtimes[internId]??emptyRuntime();
    const ordered=[...plan.blocks].sort((a,b)=>a.date.localeCompare(b.date));
    const due=ordered.filter(b=>b.date<=today&&(!active.afterDate||b.date>active.afterDate)&&!runtime.snapshots[eventId(scenario.id,b.id)]);
    if(!due.length)continue;
    const nextRuntime=clone(runtime);
    for(const b of due){const id=eventId(scenario.id,b.id);nextRuntime.snapshots[id]={scenarioId:scenario.id,block:clone(b),plan:clone(plan),next:clone(ordered.find(n=>n.date>b.date))};nextRuntime.feed.push({kind:'checkpoint',id});}
    result={...result,runtimes:{...result.runtimes,[internId]:nextRuntime}};
  }
  return result;
}
