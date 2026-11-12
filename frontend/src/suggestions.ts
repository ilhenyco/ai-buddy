import type {Block, Plan, StageKind} from './domain.ts';
import type {AppState} from './scenarios.ts';

export type SuggestionTopic = StageKind | 'general';
export type QuestionSelection = {stageKey:string;topic:SuggestionTopic;questions:string[]};

/** Product-owned copy; these questions are never configured by the manager. */
export const questionPools:Record<SuggestionTopic,readonly string[]> = {
  start:['Кто мой бадди?','Кто мой руководитель?','Как познакомиться с командой?','С чего начать первый день?'],
  week:['Как получить доступы?','Где найти рабочие инструменты?','Как подготовиться к первой задаче?','Где найти вводные курсы?'],
  adaptation:['Как обсудить цели стажировки?','Какие курсы пройти?','Как взять отгул?','Как взять отпуск?'],
  practice:['Как взять отгул?','Как взять отпуск?','Как попросить обратную связь?','К кому обратиться, если застрял в задаче?'],
  midpoint:['Как подготовиться к экватору?','Какие результаты собрать?','Что обсудить с бадди?','Как уточнить дальнейшие цели?'],
  'final-prep':['Как подготовить презентацию?','Какие результаты показать?','С кем обсудить продолжение работы?','Что обсудить с руководителем перед финалом?'],
  final:['Как представить итоги?','Как передать свои задачи?','Как получить обратную связь?','С кем обсудить следующие шаги?'],
  after:['Кому передать материалы?','К кому обратиться с оставшимися вопросами?','Как оставить обратную связь?','С кем обсудить организационные документы?'],
  general:['Где найти материалы команды?','Как связаться с бадди?','Как обсудить рабочий вопрос?','К кому обратиться за помощью?'],
};
const knownKinds = Object.keys(questionPools).filter(k=>k!=='general').sort((a,b)=>b.length-a.length) as StageKind[];

export function blockTopic(block:Block):SuggestionTopic {
  if(block.stageKind&&knownKinds.includes(block.stageKind))return block.stageKind;
  // Old v3 blocks and existing library copies do not yet have stageKind.
  return knownKinds.find(kind=>block.id===kind||block.id.startsWith(kind+'-'))??'general';
}

export function suggestionStage(plan:Plan,today:string):{stageKey:string;topic:SuggestionTopic} {
  const ordered=[...plan.blocks].sort((a,b)=>a.date.localeCompare(b.date));
  const current=ordered.filter(b=>b.date&&b.date<=today).at(-1);
  return current?{stageKey:'block:'+current.id,topic:blockTopic(current)}:{stageKey:'prestart',topic:'start'};
}

export function pickQuestions(topic:SuggestionTopic,random:()=>number=Math.random,previous:readonly string[]=[]):string[] {
  const pool=[...questionPools[topic]];
  for(let i=pool.length-1;i>0;i--){const j=Math.floor(random()*(i+1));[pool[i],pool[j]]=[pool[j],pool[i]];}
  const selected=pool.slice(0,3);
  // A new occurrence of the same template should also visibly change the set.
  if(selected.every(q=>previous.includes(q))){const replacement=pool.find(q=>!selected.includes(q));if(replacement)selected[2]=replacement;}
  return selected;
}

export function validSelection(selection:QuestionSelection|undefined,stage:{stageKey:string;topic:SuggestionTopic}):boolean {
  return !!selection&&selection.stageKey===stage.stageKey&&selection.topic===stage.topic&&
    Array.isArray(selection.questions)&&selection.questions.length===3&&new Set(selection.questions).size===3&&
    selection.questions.every(q=>questionPools[stage.topic].includes(q));
}

/** Idempotent: draw once per stage, retain order after answers, reloads and publications. */
export function ensureSuggestions(state:AppState,today:string,random:()=>number=Math.random):AppState {
  let result=state;
  for(const [internId,active] of Object.entries(state.activeScenarios)){
    const scenario=state.scenarios.find(s=>s.id===active.scenarioId&&s.internId===internId);
    if(!scenario?.published)continue;
    const stage=suggestionStage(scenario.published,today);
    const runtime=result.runtimes[internId]??{checks:{},feed:[],snapshots:{}};
    if(validSelection(runtime.suggestions,stage))continue;
    const previous=Array.isArray(runtime.suggestions?.questions)?runtime.suggestions.questions:[];
    const suggestions={...stage,questions:pickQuestions(stage.topic,random,previous)};
    result={...result,runtimes:{...result.runtimes,[internId]:{...runtime,suggestions}}};
  }
  return result;
}
