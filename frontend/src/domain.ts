export type Role = 'intern' | 'buddy' | 'lead' | 'hr';
export type StageKind = 'start' | 'week' | 'adaptation' | 'practice' | 'midpoint' | 'final-prep' | 'final' | 'after';
export type Page = 'messages' | 'plan' | 'scenarios' | 'builder' | 'knowledge' | 'components';
export type LinkKind = 'team' | 'intern' | 'buddy' | 'lead' | 'access' | 'task' | 'courses' | 'practice' | 'midpoint' | 'final' | 'handoff' | 'hr';
export type Task = { id: string; title: string; description: string; link: string; kind: LinkKind; url?: string; actions?: Partial<Record<'buddy'|'lead',string>> };
export type Block = { id: string; stageKind?: StageKind; title: string; eyebrow: string; date: string; endDate: string; intern: string; buddy: string; lead: string; tasks: Task[]; process: string };
export type Plan = { name: string; intern: string; buddy: string; lead: string; team: string; start: string; months: 6 | 9; version: number; blocks: Block[] };
export type Citation = { title: string; fragment: string; url?: string };
// server mode: citations come with the answer, answerId links a question to its answer job
export type ChatMessage = { id: string; role: 'user' | 'assistant'; text: string; source?: string; time: string; error?: boolean; citations?: Citation[]; answerId?: string };
export type Source = { id: string; title: string; category: string; body: string; team: string; status: 'ready' | 'indexing' | 'error' | 'excluded'; updated: string };
export type Access = { operationId: string | null; number: string | null; status: 'none' | 'pending' | 'approved' | 'denied' | 'unknown'; resource: string; reason: string };
export const roleNames: Record<Role, string> = { intern: 'Стажёр', buddy: 'Бадди', lead: 'Руководитель', hr: 'HR' };
export const people = { intern: 'Саша Соколов', buddy: 'Даша Морозова', lead: 'Алексей Романов', hr: 'Анна Петрова' };
export const EMPTY_ACCESS: Access = { operationId: null, number: null, status: 'none', resource: 'Репозиторий команды', reason: '' };
export function validExternalUrl(value: string) {try{const u=new URL(value);return u.protocol==='https:'||u.protocol==='http:';}catch{return false;}}

const shift = (value: string, days: number) => { const d = new Date(value + 'T12:00:00Z'); d.setUTCDate(d.getUTCDate() + days); return d.toISOString().slice(0, 10); };
const month = (value: string, count: number) => { const d = new Date(value + 'T12:00:00Z'); const day = d.getUTCDate(); d.setUTCDate(1); d.setUTCMonth(d.getUTCMonth() + count); const last = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0)).getUTCDate(); d.setUTCDate(Math.min(day, last)); return d.toISOString().slice(0, 10); };
export const dateLabel = (date: string, year = false) => {const value=new Date(date+'T12:00:00Z');return Number.isNaN(value.getTime())?'Дата не задана':new Intl.DateTimeFormat('ru-RU',{day:'numeric',month:'long',...(year?{year:'numeric' as const}:{})}).format(value);};
export const timeLabel = () => new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit' }).format(new Date());
export function shiftPlanStart(plan: Plan, start: string): Plan {
  if(!start)return {...plan,start};
  const old=plan.start||plan.blocks[0]?.date||start;
  const days=Math.round((Date.parse(start+'T12:00:00Z')-Date.parse(old+'T12:00:00Z'))/86_400_000);
  return {...plan,start,blocks:plan.blocks.map(b=>({...b,date:shift(b.date,days),endDate:shift(b.endDate,days)}))};
}
const task = (id: string, title: string, description: string, link: string, kind: LinkKind): Task => ({ id, title, description, link, kind });

const helperActions: Record<string,Partial<Record<'buddy'|'lead',string>>> = {
  hello:{buddy:'Познакомь стажёра с командой и расскажи, кто чем занимается.',lead:'Представь стажёра команде и объясни её задачи.'},
  'meet-buddy':{buddy:'Свяжись со стажёром и договоритесь, как будете общаться.'},
  'meet-lead':{lead:'Договорись со стажёром о первой встрече: обсудите ожидания и план.'},
  channels:{buddy:'Помоги стажёру найти основные командные чаты.'},
  access:{buddy:'Вместе со стажёром определите нужные доступы и пройдите подачу заявки.',lead:'Если нужно согласование, помоги стажёру получить доступы для задачи.'},
  tools:{buddy:'Покажи стажёру инструменты и вместе проверьте рабочее окружение.'},
  'first-task':{buddy:'Вместе прочитайте первую задачу и разберите вопросы стажёра.',lead:'Обсуди со стажёром постановку первой задачи и ожидаемый результат.'},
  goals:{buddy:'Помоги стажёру подготовить вопросы о целях стажировки.',lead:'Вместе со стажёром согласуйте цели и ожидаемые результаты.'},
  courses:{buddy:'Помоги стажёру найти вводные курсы и разобраться с вопросами.'},
  feedback:{buddy:'Обсудите со стажёром первые впечатления и где нужна помощь.'},
  'practice-info':{buddy:'Если практика нужна, помоги стажёру найти требования и контакт HR.',lead:'Если практика нужна, обсудите со стажёром подходящие задачи.'},
  'practice-hr':{buddy:'При необходимости помоги стажёру уточнить у HR документы и сроки.'},
  'mid-results':{buddy:'Вместе со стажёром выберите примеры работы для промежуточных итогов.'},
  'mid-buddy':{buddy:'Обсудите результаты стажёра, трудности и дальнейшую траекторию.'},
  'mid-lead':{lead:'Договоритесь о промежуточной встрече, обсудите результаты и уточните цели.'},
  format:{lead:'Обсуди со стажёром формат финала, критерии и срок дальнейшего ответа.'},
  examples:{buddy:'Вместе со стажёром выберите примеры его вклада для финала.'},
  rehearsal:{buddy:'Если стажёру нужна репетиция, договоритесь о времени и проведите её.'},
  present:{buddy:'Поддержи стажёра в подготовке итогового рассказа.',lead:'Обсуди со стажёром итоги и дай обратную связь.'},
  handoff:{buddy:'Вместе со стажёром проверьте контекст и ссылки для передачи задач.',lead:'Согласуйте со стажёром получателей оставшихся задач.'},
  outcome:{lead:'Сообщи стажёру согласованный дальнейший шаг или дату ответа.'},
  verify:{buddy:'Вместе со стажёром проверьте, что у открытых задач есть получатели.'},
  documents:{buddy:'Помоги стажёру найти контакт HR по оставшимся документам.'},
};

export function tasksForRole(block: Block, role: Role): (Task & {step:number})[] {
  if(role==='hr')return [];
  return block.tasks.flatMap((t,index)=>{
    if(role==='intern')return [{...t,step:index+1}];
    const action=t.actions?.[role]??helperActions[t.id]?.[role]??'';
    if(!action.trim())return [];
    return [{...t,title:action,description:`Пункт ${index+1} стажёра: «${t.title}»`,step:index+1,link:t.kind==='buddy'||t.kind==='lead'?'Контакт стажёра':t.link,kind:t.kind==='buddy'||t.kind==='lead'?'intern' as const:t.kind}];
  });
}

export function dueBlocks(plan: Plan, today: string, deliveredIds: string[]) {
  return plan.blocks.filter(b=>b.date<=today&&!deliveredIds.includes(b.id)).sort((a,b)=>a.date.localeCompare(b.date));
}

export function makePlan(months: 6 | 9 = 6): Plan {
  const start = '2026-11-11'; const end = month(start, months);
  const definitions: Omit<Block, 'date' | 'endDate'>[] = [
    { id: 'start', title: 'Первый день', eyebrow: 'Добро пожаловать в команду', process: 'P02 · P03', intern: 'Саша, привет! Я Хомыч, твой AI-бадди. Помогу разобраться, как всё устроено, и вовремя напомню о важном. Начнём со знакомства — рядом всегда есть люди, которые помогут.', buddy: 'Сегодня у Саши первый день. Познакомь его с командой, помоги найти основные чаты и договоритесь, как будете общаться.', lead: 'Саша выходит на стажировку. Найди время познакомиться, обсудить ожидания и рассказать о команде. Даша поможет с повседневными вопросами.', tasks: [
      task('hello','Познакомься с командой','Кто чем занимается и в каких чатах общаемся.','О команде','team'),
      task('meet-buddy','Напиши своему бадди','Даша поможет с первыми шагами и вопросами.','Контакт Даши','buddy'),
      task('meet-lead','Договорись о встрече с руководителем','Обсудите ожидания и план стажировки.','Контакт Алексея','lead'),
    ] },
    { id: 'week', title: 'Первая неделя', eyebrow: 'Осваиваемся в Авито', process: 'P03–P07', intern: 'Саша, пора освоиться в команде. На эту неделю — несколько первых шагов. С доступами и инструментами поможет Даша, а если что-то непонятно, просто спроси меня.', buddy: 'У Саши начинается знакомство с инструментами. Помоги получить нужные доступы, покажи рабочий процесс и вместе разберите первую задачу.', lead: 'На этой неделе Саша получает доступы и готовится к первой задаче. Помоги с согласованием, если потребуется, и обсуди постановку и ожидаемый результат.', tasks: [
      task('channels','Найди командные чаты','Каналы, встречи и полезные контакты в одном месте.','Каналы команды','team'),
      task('access','Получи рабочие доступы','Пройди этот шаг вместе с Дашей.','Инструкция по доступам','access'),
      task('tools','Познакомься с инструментами','Даша покажет, как устроена ежедневная работа.','Написать бадди','buddy'),
      task('first-task','Подготовься к первой задаче','Прочитай постановку и обсуди вопросы с бадди.','Открыть инструкцию','task'),
    ] },
    { id: 'adaptation', title: 'Цели и обучение', eyebrow: 'Первые недели', process: 'P08', intern: 'Ты уже немного освоился. Самое время обсудить цели с Алексеем и пройти вводные материалы. Результаты и договорённости фиксируйте в привычных инструментах команды.', buddy: 'Помоги Саше разобраться с первыми задачами и командными инструкциями. Обсудите, где пока нужна поддержка.', lead: 'Обсуди с Сашей первые впечатления, цели и ожидаемые результаты. Уточните приоритеты и договоритесь о следующем разговоре.', tasks: [
      task('goals','Обсуди цели стажировки','Согласуйте с руководителем, чего хочется достичь.','Связаться с руководителем','lead'),
      task('courses','Пройди вводные курсы','Ссылки и сроки — в памятке по адаптации.','Материалы обучения','courses'),
      task('feedback','Расскажи, как идёт адаптация','Что уже получилось и где нужна помощь.','Написать бадди','buddy'),
    ] },
    { id: 'practice', title: 'Учебная практика', eyebrow: 'Если это актуально для тебя', process: 'P09', intern: 'Если стажировка засчитывается как учебная практика, заранее уточни у HR, какие документы нужны и в какие сроки. Если практика не нужна, этот пункт можно пропустить.', buddy: 'Если Саше нужна учебная практика, помоги найти контакт HR и сориентироваться в командных материалах.', lead: 'При необходимости обсудите с Сашей задачи для учебной практики. Оформление и требования учебного заведения он согласует с HR.', tasks: [
      task('practice-info','Уточни требования к практике','Порядок оформления и документы обсуждаются с HR.','Памятка по практике','practice'),
      task('practice-hr','Согласуй документы и сроки','Этот шаг нужен только при наличии учебной практики.','Контакт HR','hr'),
    ] },
    { id: 'midpoint', title: 'Экватор стажировки', eyebrow: 'Половина пути позади', process: 'P10', intern: 'Пора остановиться и посмотреть, что уже получилось. Собери примеры работы, обсуди их с Дашей и договорись о встрече с Алексеем. Презентацию и выводы готовь там, где тебе удобно.', buddy: 'У Саши экватор стажировки. Помоги выбрать примеры работы, обсудить трудности и при необходимости отрепетировать разговор о результатах.', lead: 'Пришло время промежуточной встречи с Сашей. Обсудите результаты, дай обратную связь и уточните цели на оставшуюся часть стажировки.', tasks: [
      task('mid-results','Собери промежуточные результаты','Примеры задач, вклад и вопросы для обсуждения.','Как подготовиться','midpoint'),
      task('mid-buddy','Обсуди результаты с бадди','Даша поможет посмотреть на работу со стороны.','Контакт бадди','buddy'),
      task('mid-lead','Договорись о промежуточной встрече','Обсудите прогресс и дальнейшие цели.','Контакт руководителя','lead'),
    ] },
    { id: 'final-prep', title: 'Подготовка к финалу', eyebrow: 'Месяц до завершения', process: 'P11', intern: 'Финал уже близко. Обсуди с Алексеем формат итоговой встречи, критерии и срок ответа. С Дашей выбери примеры работ и подготовь рассказ о своём вкладе.', buddy: 'Помоги Саше выбрать примеры для финального выступления. При необходимости проведите репетицию и обсудите передачу открытых задач.', lead: 'Заранее обсуди с Сашей формат финала, критерии оценки и когда будет известен дальнейший шаг. Кадровые вопросы согласуются с HR.', tasks: [
      task('format','Уточни формат и критерии финала','Что показать и когда ожидать дальнейший ответ.','Связаться с руководителем','lead'),
      task('examples','Выбери примеры своих работ','Подготовь результаты вместе с бадди.','Памятка к финалу','final'),
      task('rehearsal','Договорись о репетиции с бадди','Если хочется потренировать выступление.','Контакт бадди','buddy'),
    ] },
    { id: 'final', title: 'Итоги и передача', eyebrow: 'Последняя неделя', process: 'P12', intern: 'Время рассказать о результатах и передать команде открытые задачи. Не забудь ссылки, контекст и договорённости. Итоги вы обсудите лично — загружать отчёт сюда не нужно.', buddy: 'Помоги Саше передать команде открытые задачи. Проверьте ссылки, контекст и получателей работы.', lead: 'Проведи итоговое обсуждение с Сашей. Сообщи согласованный дальнейший шаг или дату, когда сможете вернуться с ответом.', tasks: [
      task('present','Представь результаты команде','Сравни результат с согласованными целями.','Памятка к выступлению','final'),
      task('handoff','Передай открытые задачи','Ссылки, статус работы и контекст для получателей.','Чек-лист передачи','handoff'),
      task('outcome','Обсуди дальнейшие шаги','Решение или срок ответа сообщит руководитель.','Контакт руководителя','lead'),
    ] },
    { id: 'after', title: 'После стажировки', eyebrow: 'Следующие шаги', process: 'P13', intern: 'Спасибо за этот путь! Проверь, что команда получила все задачи. По документам и опросу обратись к HR. Если дальнейшее решение ещё ожидается, уточни срок у руководителя.', buddy: 'Проверьте с Сашей, что команда приняла открытые задачи и знает, где найти материалы.', lead: 'Убедись, что Саше понятны дальнейшие шаги. Если решение ещё согласуется, сообщи ему актуальный срок ответа лично.', tasks: [
      task('verify','Проверь передачу задач','У каждой открытой задачи есть получатель.','Памятка по передаче','handoff'),
      task('documents','Уточни документы и обратную связь','Порядок оформления и ссылку на опрос подскажет HR.','Связаться с HR','hr'),
    ] },
  ];
  const dates = [start, shift(start,7),shift(start,14),month(start,2), months===6?month(start,3):shift(month(start,4),15),month(end,-1),shift(end,-7),shift(end,1)];
  const shortIntros:Record<string,string>={start:'Саша, привет! Я Хомыч. Сегодня знакомимся с командой — начни с этих шагов.',week:'Саша, началась первая неделя. Пора получить доступы и разобраться с первой задачей вместе с Дашей.',adaptation:'Пора обсудить цели стажировки и пройти вводные материалы. Вот ближайшие шаги.',practice:'Если тебе нужна учебная практика, пора уточнить документы и сроки у HR.',midpoint:'Половина стажировки позади. Пора собрать результаты и обсудить их с бадди и руководителем.','final-prep':'До финала месяц. Пора уточнить формат итогов и подготовить примеры своей работы.',final:'Пора обсудить итоги и передать открытые задачи. Результаты готовь в привычных инструментах команды.',after:'Спасибо за этот путь! Проверь передачу задач и оставшиеся организационные вопросы.'};
  return { name: 'Backend · стажировка', intern: people.intern, buddy: people.buddy, lead: people.lead, team: 'Backend · Платформа', start, months, version: 1, blocks: definitions.map((b,i)=>({...b,stageKind:b.id as StageKind,intern:shortIntros[b.id],buddy:`У Саши — ${b.title.toLowerCase()}. Помоги ему с этими пунктами:`,lead:`У Саши — ${b.title.toLowerCase()}. Твоё участие в его шагах:`,tasks:b.tasks.map(t=>({...t,actions:helperActions[t.id]??{}})),date:dates[i],endDate:dates[i+1]??shift(dates[i],7)})) };
}

export const seedSources: Source[] = [
  { id:'access', title:'Как получить рабочие доступы', category:'Инструменты', team:'platform', status:'ready', updated:'10 ноября', body:'Демонстрационная памятка. Сначала уточни у бадди список нужных инструментов. Затем открой согласованный сервис заявок, укажи ресурс и объясни, для какой задачи нужен доступ. Если согласование затянулось, свяжись с бадди. Ссылка на реальный корпоративный сервис в макете не задана.' },
  { id:'team', title:'Команда и полезные контакты', category:'Знакомство', team:'platform', status:'ready', updated:'10 ноября', body:'Демонстрационные данные. Даша Морозова — бадди: помогает с задачами и знакомством. Алексей Романов — руководитель: обсуждает цели и важные решения. Анна Петрова — контакт HR по организационным вопросам. Корпоративные чаты добавляет команда.' },
  { id:'learning', title:'Первые недели и обучение', category:'Адаптация', team:'platform', status:'ready', updated:'9 ноября', body:'Демонстрационная памятка. Ссылки на вводные курсы и сроки передаёт HR. Обсуди цели с руководителем, а вопросы по инструментам — с бадди. Отметки чек-листа нужны для собственного учёта и не задерживают следующие периоды.' },
  { id:'midpoint', title:'Подготовка к экватору', category:'Встречи', team:'platform', status:'ready', updated:'9 ноября', body:'Демонстрационная памятка. Собери примеры выполненных задач, сравни их с целями и запиши вопросы. Обсуди материалы с бадди и договорись о встрече с руководителем. Подготовка презентации и выводы выполняются вне бота.' },
  { id:'foreign', title:'Внутренние материалы другой команды', category:'Закрытый источник', team:'other', status:'ready', updated:'8 ноября', body:'Этот источник не должен попадать в ответы стажёра команды platform.' },
];

export function demoAnswer(query: string, sources: Source[], mode: string, participants: Pick<Plan,'buddy'|'lead'>={buddy:people.buddy,lead:people.lead}): Pick<ChatMessage,'text'|'source'|'error'> {
  if(mode==='model-error')return {text:'Сейчас не получается подготовить ответ. Попробуй ещё раз чуть позже. Если вопрос срочный, обратись к своему бадди.',error:true};
  const q=query.toLowerCase();
  const id= /доступ|репозитор|инструмент|vpn/.test(q)?'access':/бадди|команд|руковод|контакт|даш/.test(q)?'team':/курс|обуч|цел|чек/.test(q)?'learning':/экватор|результат|встреч|финал/.test(q)?'midpoint':null;
  const allowed=sources.filter(s=>s.team==='platform'&&s.status==='ready');
  const words=q.split(/[^а-яёa-z0-9]+/i).filter(w=>w.length>=4);
  const matched=allowed.map(s=>({s,score:words.filter(w=>(s.title+' '+s.body).toLowerCase().includes(w)).length})).sort((a,b)=>b.score-a.score)[0];
  const specificMatch=matched?.score>=2&&!['access','team','learning','midpoint'].includes(matched.s.id)?matched.s:undefined;
  const source=specificMatch??allowed.find(s=>s.id===id)??(matched?.score>=2?matched.s:undefined);
  if(!source || mode==='no-source')return {text:'В доступных материалах я не нашёл подтверждённого ответа. Уточни вопрос или обсуди его с бадди. Если это вопрос о документах, лучше обратиться к HR.'};
  const replies: Record<string,string>={
    access:'Начни со своего бадди: вместе определите, какие инструменты нужны для первой задачи. Затем открой инструкцию по доступам и форму в согласованном сервисе. Если заявка ожидает согласования, можно пока знакомиться с командой и материалами — следующий период придёт по расписанию.',
    team:`Твой бадди — ${participants.buddy}: поможет с инструментами, задачами и знакомством с командой. Руководитель — ${participants.lead}: с ним обсуждаются цели и важные решения. Контакты доступны в блоке «Твоя команда».`,
    learning:'Ссылки на вводные курсы и сроки предоставляет HR. Цели стажировки обсуди с Алексеем. Чек-листы помогают ничего не забыть: их отметки не блокируют следующие периоды.',
    midpoint:'Собери примеры задач, свой вклад и вопросы. Сначала обсуди материалы с Дашей, затем договорись о встрече с Алексеем. Презентацию и выводы готовь в привычных инструментах команды — загружать их в бот не нужно.',
  };
  return {text:replies[source.id]??`В материале «${source.title}» указано: ${source.body}`,source:source.id};
}

export function submitAccess(previous: Access, reason: string): Access {
  if(previous.operationId)return previous;
  return {...previous,operationId:'demo-access-001',number:'DEMO-1042',status:'pending',reason};
}
export function visibleSources(sources: Source[]) {return sources.filter(s=>s.team==='platform'&&s.status!=='excluded');}
export function roleBody(block: Block, role: Role, plan: Plan) {
  const raw=role==='hr'?'Организационные материалы и сроки передаются руководителю. Конкретная рассылка HR пока требует согласования.':block[role];
  return personalizeText(raw,plan);
}
export function personalizeText(raw: string, plan: Plan) {
  let text=raw;
  const internName=plan.intern||'[Имя стажёра]';
  if(plan.intern!==people.intern)text=text.replaceAll('У Саши',`У стажёра ${internName}`).replaceAll('у Саши',`у стажёра ${internName}`).replaceAll('с Сашей',`со стажёром ${internName}`).replaceAll('Саше',`стажёру ${internName}`).replaceAll('Саша',plan.intern?plan.intern.split(' ')[0]:internName);
  if(plan.buddy!==people.buddy)text=text.replaceAll('с Дашей',`с бадди ${plan.buddy}`).replaceAll('Дашей',plan.buddy).replaceAll('Даши',plan.buddy).replaceAll('Даше',plan.buddy).replaceAll('Даша',plan.buddy.split(' ')[0]);
  return text;
}
