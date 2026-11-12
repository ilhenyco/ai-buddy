"""Build the proposed product API; JSON is valid YAML 1.2 and avoids parser drift."""
from pathlib import Path
import json, shutil, re
R=Path(__file__).resolve().parents[1]
D=R/'docs/ai-buddy/system-analysis'
def ref(name):return {'$ref':'#/components/schemas/'+name}
def arr(name):return {'type':'array','items':ref(name)}
def obj(props,required=None):return {'type':'object','properties':props,'required':list(props) if required is None else required,'additionalProperties':False}
def string(**kw):return {'type':'string',**kw}
ID=string(minLength=1)
DT=string(format='date-time')
DAY=string(format='date')
NID={'type':['string','null']}
SC={}
SC['Error']=obj({'code':string(),'message':string(),'requestId':ID,'details':{'type':'object','additionalProperties':True}},['code','message','requestId'])
SC['Person']=obj({'id':ID,'displayName':string()})
SC['Assignment']=obj({'id':ID,'intern':ref('Person'),'buddy':ref('Person'),'lead':ref('Person'),'teamId':ID,'capabilities':{'type':'array','items':string()}})
SC['Context']=obj({'subject':ref('Person'),'assignments':arr('Assignment'),'capabilities':{'type':'array','items':string()},'serverTime':DT,'csrfToken':ID})
SC['RoleActions']=obj({'buddy':string(),'lead':string()},[])
SC['Task']=obj({'id':ID,'title':string(),'description':string(),'kind':string(enum=['team','buddy','lead','hr','access','task','courses','practice','midpoint','final','handoff']),'link':string(),'url':string(),'actions':ref('RoleActions')},['id','title','description','kind','link'])
SC['Block']=obj({'id':ID,'stageKind':string(enum=['start','week','adaptation','practice','midpoint','final-prep','final','after','generic']),'title':string(),'date':DAY,'endDate':DAY,'intern':string(),'buddy':string(),'lead':string(),'tasks':arr('Task')},['id','title','date','intern','buddy','lead','tasks'])
SC['Plan']=obj({'name':string(),'internId':NID,'buddyId':NID,'leadId':NID,'teamId':ID,'start':DAY,'months':{'type':'integer','enum':[6,9]},'blocks':arr('Block')})
SC['Revision']=obj({'id':ID,'number':{'type':'integer','minimum':1},'publishedAt':DT,'plan':ref('Plan')})
SC['Scenario']=obj({'id':ID,'assignmentId':NID,'draft':ref('Plan'),'draftVersion':{'type':'integer','minimum':1},'published':{'anyOf':[ref('Revision'),{'type':'null'}]},'isActive':{'type':'boolean'},'hasUnpublishedChanges':{'type':'boolean'},'updatedAt':DT})
SC['ScenarioPage']=obj({'items':arr('Scenario'),'nextCursor':NID,'counts':obj({'all':{'type':'integer'},'drafts':{'type':'integer'},'published':{'type':'integer'}})})
SC['CreateScenario']=obj({'assignmentId':NID,'months':{'type':'integer','enum':[6,9]},'start':DAY,'name':string()},['months','start','name'])
SC['SaveDraft']=obj({'plan':ref('Plan')})
SC['Publish']=obj({'draftVersion':{'type':'integer','minimum':1},'expectedActiveScenarioId':NID,'confirmReplacement':{'type':'boolean'}})
SC['Publication']=obj({'scenario':ref('Scenario'),'replacedScenarioId':NID,'cutoffDate':{'type':['string','null'],'format':'date'},'activeRevisionId':ID})
SC['Template']=obj({'id':ID,'months':{'type':'integer','enum':[6,9]},'blocks':arr('Block'),'poolVersion':ID})
SC['TemplatePage']=obj({'items':arr('Template')})
SC['Citation']=obj({'sourceId':ID,'sourceVersionId':ID,'title':string(),'fragment':string()})
SC['Message']=obj({'id':ID,'assignmentId':ID,'kind':string(enum=['checkpoint','user','assistant']),'recipientUserId':ID,'createdAt':DT,'text':string(),'scenarioId':ID,'revisionId':ID,'block':ref('Block'),'citations':arr('Citation'),'answerRequestId':ID,'errorCode':string()},['id','assignmentId','kind','recipientUserId','createdAt','text'])
SC['FeedPage']=obj({'items':arr('Message'),'nextCursor':NID,'checks':{'type':'array','items':ref('Check')},'serverTime':DT})
SC['Check']=obj({'messageId':ID,'taskId':ID,'userId':ID,'checked':{'type':'boolean'},'updatedAt':DT})
SC['SetCheck']=obj({'checked':{'type':'boolean'}})
SC['Ask']=obj({'text':string(minLength=1,maxLength=10000)})
SC['AnswerJob']=obj({'id':ID,'assignmentId':ID,'questionMessageId':ID,'status':string(enum=['queued','running','completed','failed']),'answer':{'anyOf':[ref('Message'),{'type':'null'}]},'error':{'anyOf':[ref('Error'),{'type':'null'}]},'createdAt':DT})
SC['SuggestionSelection']=obj({'stageKey':ID,'stageKind':string(),'poolVersion':ID,'questions':{'type':'array','minItems':3,'maxItems':3,'uniqueItems':True,'items':string()},'selectedAt':DT})
SC['SourceACL']=obj({'teamIds':{'type':'array','items':ID},'allowedRoleKinds':{'type':'array','items':string()}},['teamIds'])
SC['SourceInput']=obj({'title':string(minLength=1),'body':string(minLength=1,maxLength=1000000),'category':string(),'acl':ref('SourceACL')})
SC['Source']=obj({'id':ID,'versionId':NID,'title':string(),'body':string(),'category':string(),'acl':ref('SourceACL'),'status':string(enum=['ready','indexing','error','excluded']),'updatedAt':DT,'etag':ID})
SC['SourcePage']=obj({'items':arr('Source'),'nextCursor':NID})
SC['IndexJob']=obj({'id':ID,'sourceId':ID,'candidateVersionId':ID,'status':string(enum=['queued','running','ready','failed']),'activeVersionId':NID,'error':{'anyOf':[ref('Error'),{'type':'null'}]}})
SC['Notification']=obj({'id':ID,'messageId':ID,'assignmentId':ID,'title':string(),'createdAt':DT,'deliveryStatus':string(enum=['queued','sending','sent','unknown','failed'])})
SC['NotificationPage']=obj({'items':arr('Notification'),'nextCursor':NID})
SC['AccessDraftInput']=obj({'assignmentId':ID,'resourceId':ID,'reason':string(minLength=1)})
SC['AccessDraft']=obj({'id':ID,'assignmentId':ID,'resourceId':ID,'reason':string(),'version':{'type':'integer','minimum':1},'expiresAt':DT})
SC['ConfirmAccess']=obj({'draftVersion':{'type':'integer','minimum':1},'reason':string(minLength=1),'confirmed':{'const':True}})
SC['AccessOperation']=obj({'id':ID,'assignmentId':ID,'resourceId':ID,'correlationId':ID,'externalNumber':NID,'status':string(enum=['pending','approved','denied','unknown']),'submissionConfirmed':{'type':'boolean'},'updatedAt':DT})
SC['ProtectedTask']=obj({'id':ID,'title':string(),'description':string(),'requiredResourceId':ID})

EX={
 'CreateScenario':{'assignmentId':'assignment-sasha','months':6,'start':'2026-11-11','name':'Backend · Платформа'},
 'SaveDraft':{'plan':{'name':'Backend · Платформа','internId':'u-sasha','buddyId':'u-dasha','leadId':'u-alex','teamId':'platform','start':'2026-11-11','months':6,'blocks':[{'id':'week','stageKind':'week','title':'Первая неделя','date':'2026-11-18','intern':'Пора получить доступы.','buddy':'Помоги с доступами.','lead':'Помоги при согласовании.','tasks':[{'id':'access','title':'Получи доступы','description':'Вместе с бадди','kind':'access','link':'Инструкция','url':'https://example.org/demo-access','actions':{'buddy':'Помоги уточнить список ресурсов.'}}]}]}},
 'Publish':{'draftVersion':4,'expectedActiveScenarioId':'scenario-old','confirmReplacement':True},
 'SetCheck':{'checked':True},'Ask':{'text':'Как получить доступы?'},
 'SourceInput':{'title':'Доступы','body':'Синтетическая инструкция: уточни список у бадди.','category':'Инструменты','acl':{'teamIds':['platform'],'allowedRoleKinds':['intern','buddy','lead']}},
 'AccessDraftInput':{'assignmentId':'assignment-sasha','resourceId':'repo-learning-read','reason':'Для первой задачи'},
 'ConfirmAccess':{'draftVersion':1,'reason':'Для первой задачи','confirmed':True},
 'Error':{'code':'VERSION_CONFLICT','message':'Черновик изменён. Обновите редакцию перед публикацией.','requestId':'req-example','details':{'currentVersion':5}},
 'SuggestionSelection':{'stageKey':'block:week','stageKind':'week','poolVersion':'2026-11-12','questions':['Как получить доступы?','Где найти рабочие инструменты?','Как подготовиться к первой задаче?'],'selectedAt':'2026-11-18T06:00:00Z'},
 'Check':{'messageId':'message-week','taskId':'access','userId':'u-sasha','checked':True,'updatedAt':'2026-11-18T08:00:00Z'},
 'AccessOperation':{'id':'op-1','assignmentId':'assignment-sasha','resourceId':'repo-learning-read','correlationId':'corr-1','externalNumber':'MOCK-1042','status':'pending','submissionConfirmed':True,'updatedAt':'2026-11-18T08:00:00Z'},
 'AnswerJob':{'id':'answer-1','assignmentId':'assignment-sasha','questionMessageId':'m-1','status':'queued','answer':None,'error':None,'createdAt':'2026-11-18T08:00:00Z'},
 'IndexJob':{'id':'index-1','sourceId':'s-1','candidateVersionId':'sv-2','status':'queued','activeVersionId':'sv-1','error':None},
 'AccessDraft':{'id':'ad-1','assignmentId':'assignment-sasha','resourceId':'repo-learning-read','reason':'Для первой задачи','version':1,'expiresAt':'2026-11-18T09:00:00Z'},
 'ProtectedTask':{'id':'task-first','title':'Первая задача','description':'Демонстрационная постановка','requiredResourceId':'repo-learning-read'},
}
def sample(schema):
    if '$ref' in schema:return sample(SC[schema['$ref'].rsplit('/',1)[-1]])
    if 'example' in schema:return schema['example']
    if 'const' in schema:return schema['const']
    if 'enum' in schema:return schema['enum'][0]
    if 'anyOf' in schema:return sample(schema['anyOf'][0])
    typ=schema.get('type','string')
    if isinstance(typ,list):return None if 'null' in typ else 'example'
    if typ=='object':return {k:sample(v) for k,v in schema.get('properties',{}).items() if k in schema.get('required',[])}
    if typ=='array':return [sample(schema['items'])]
    if typ=='integer':return schema.get('minimum',1)
    if typ=='boolean':return False
    if schema.get('format')=='date':return '2026-11-18'
    if schema.get('format')=='date-time':return '2026-11-18T08:00:00Z'
    return 'example'
for n,ex in EX.items():SC[n]['example']=ex
for n in SC:
    if n not in EX:EX[n]=sample(SC[n]);SC[n]['example']=EX[n]

P={}
def op(path,method,id,title,out,body=None,code=200,write=False,etag=False,desc=''):
    params=[]
    for p in re.findall(r'\{(\w+)\}',path):params.append({'name':p,'in':'path','required':True,'schema':ID})
    if method=='get' and out.endswith('Page'):
        params += [{'name':'cursor','in':'query','schema':string()},{'name':'limit','in':'query','schema':{'type':'integer','minimum':1,'maximum':100,'default':30}}]
    if id in ('listScenarios','listSources'):params.append({'name':'q','in':'query','schema':string()})
    if id=='listScenarios':params.append({'name':'filter','in':'query','schema':string(enum=['all','drafts','published'],default='all')})
    if id=='getSource':params.append({'name':'versionId','in':'query','schema':ID,'description':'Историческая цитата; текущий ACL всё равно проверяется.'})
    if write:params.append({'name':'Idempotency-Key','in':'header','required':True,'schema':ID,'description':'Стабильный ключ попытки для subject+operation+canonical payload; повтор с другим payload даёт 409.'})
    if etag:params.append({'name':'If-Match','in':'header','required':True,'schema':ID,'description':'ETag последней прочитанной редакции, без wildcard.'})
    if method!='get':params.append({'name':'X-CSRF-Token','in':'header','required':True,'schema':ID,'description':'Токен из sessionContext, связанный с cookie-сессией; дополнительно проверяется Origin.'})
    content={'application/json':{'schema':ref(out)}}
    if out in EX:content['application/json']['example']=EX[out]
    responses={str(code):{'description':'Успех / принятое долговечное задание' if code==202 else 'Успех','content':content}}
    if out in ('Scenario','Source'):responses[str(code)]['headers']={'ETag':{'schema':string(),'description':'Версия для следующего If-Match'}}
    for status,codeName in [(401,'UNAUTHENTICATED'),(403,'FORBIDDEN'),(404,'NOT_FOUND'),(409,'CONFLICT'),(412,'VERSION_CONFLICT'),(422,'VALIDATION_ERROR'),(429,'RATE_LIMITED'),(503,'DEPENDENCY_UNAVAILABLE')]:
        responses[str(status)]={'description':codeName,'content':{'application/json':{'schema':ref('Error'),'example':{'code':codeName,'message':'Безопасное описание ошибки; введённые данные клиента сохраняются.','requestId':'req-example'}}}}
    entry={'operationId':id,'summary':title,'description':desc or 'Проект будущего Go API. Сервер проверяет subject, назначение и права; frontend пока не вызывает этот endpoint.','parameters':params,'responses':responses,'security':[{'sessionCookie':[]}]}
    if body:
        entry['requestBody']={'required':True,'content':{'application/json':{'schema':ref(body)}}}
        if body in EX:entry['requestBody']['content']['application/json']['example']=EX[body]
    P.setdefault(path,{})[method]=entry

op('/session/context','get','sessionContext','Личность, назначения, разрешённые контакты и capabilities','Context')
op('/scenario-templates','get','listTemplates','Встроенные блоки 6/9 месяцев; подсказки не редактируются руководителем','TemplatePage')
op('/scenarios','get','listScenarios','Список собственных персональных сценариев','ScenarioPage')
op('/scenarios','post','createScenario','Создать неполный черновик из шаблона','Scenario','CreateScenario',201,True)
op('/scenarios/{scenarioId}','get','getScenario','Черновик и опубликованная версия своего сценария','Scenario')
op('/scenarios/{scenarioId}/draft','put','saveDraft','Сохранить целый структурно корректный, но неполный draft','Scenario','SaveDraft',200,False,True,
   'Пустые поля допустимы в draft. Валидация структуры, URL и назначения обязательна. Опубликованного стажёра менять нельзя. If-Match предотвращает потерю конкурирующей правки. PUT идемпотентен по содержимому.')
op('/scenarios/{scenarioId}/publications','post','publishScenario','Атомарно опубликовать и активировать вариант','Publication','Publish',201,True,True,
   'Проверить полные обязательные поля и draftVersion. expectedActiveScenarioId должен совпадать с текущим. При другой активной записи требуется confirmReplacement=true; конфликт 409. Замена сохраняет cutoff по локальной календарной дате назначения. Старые снимки неизменны. Idempotency-Key возвращает ту же revision при повторе.')
op('/assignments/{assignmentId}/messages','get','listMessages','Адресованная пользователю лента, без чужого личного чата','FeedPage')
op('/messages/{messageId}/tasks/{taskId}/check','put','setCheck','Поставить свою формальную отметку','Check','SetCheck',desc='userId берётся только из сессии. Task принадлежит видимому снимку адресата. Отметка не запускает и не блокирует этап или инструмент.')
op('/assignments/{assignmentId}/suggestions','get','getSuggestions','Сохранённая тройка текущего этапа','SuggestionSelection',desc='Только стажёру своего назначения. Выбор и сохранение атомарны при смене stageKey. До старта startup pool; без активной публикации 409 NO_ACTIVE_SCENARIO. Стабильный stageKey не зависит от revision при правке той же стадии. GET логически читает стабильный ресурс; ленивое создание эквивалентно идемпотентному upsert.')
op('/assignments/{assignmentId}/questions','post','askQuestion','Сохранить личный вопрос и создать AnswerJob','AnswerJob','Ask',202,True)
op('/answers/{answerId}','get','getAnswer','Получить состояние и сохранённый ответ','AnswerJob',desc='Только автор вопроса. completed может содержать честный fallback без цитаты. failed содержит безопасную ошибку, вопрос не теряется. Клиент завершает polling при completed/failed, использует Retry-After при 429/503.')
op('/sources','get','listSources','Разрешённый список источников; управление по capability','SourcePage')
op('/sources','post','createSource','Создать текстовый источник и задание индексации','IndexJob','SourceInput',202,True)
op('/sources/{sourceId}','get','getSource','Полный разрешённый текст/версия для чтения','Source')
op('/sources/{sourceId}','put','updateSource','Подготовить новую версию; старая активна до ready','IndexJob','SourceInput',202,True,True)
op('/sources/{sourceId}/exclude','post','excludeSource','Исключить из новых ответов без удаления истории','Source',write=True,etag=True)
op('/source-jobs/{jobId}','get','getSourceJob','Состояние индексирования','IndexJob')
op('/notifications','get','listNotifications','Свои сообщения и состояния доставки','NotificationPage')
op('/access-drafts','post','createAccessDraft','Условная интеграция: подготовить черновик действия','AccessDraft','AccessDraftInput',201,True)
op('/access-drafts/{draftId}/confirm','post','confirmAccess','Подтверждение человеком и единственная отправка в HTTP-мок','AccessOperation','ConfirmAccess',202,True,
   desc='Функция условна до OQ08. Подтверждение связано с автором и версией черновика. Сначала долговечно сохранить operation/correlationId, затем вызывать mock. Потеря ответа -> unknown и сверка по correlationId, не новая заявка. Повтор ключа с тем же payload возвращает тот же результат. Модель не вызывает confirm за пользователя.')
op('/access-operations/{operationId}','get','getAccess','Номер и статус той же внешней операции','AccessOperation')
op('/assignments/{assignmentId}/protected-first-task','get','getProtectedTask','Защищённая постановка только при актуальном праве нужного ресурса','ProtectedTask',desc='Условно до OQ08. Сервер сверяет resourceId и entitlement, а не произвольный approved и не отметку. При отсутствии права 403 ACCESS_REQUIRED. Остальная переписка доступна.')

def main():
    D.mkdir(parents=True,exist_ok=True)
    old=D/'openapi.yaml'; history=D/'openapi-legacy.yaml'
    if old.exists() and not history.exists():shutil.copy2(old,history)
    doc={'openapi':'3.1.0','info':{'title':'Хомыч — проект API для передачи FRONT','version':'1.0.0-proposed','description':'Не реализованный серверный контракт 12.11.2026. JSON является YAML 1.2. FRONT пока использует localStorage и demoAnswer. Лимиты текста/страниц — предложения для проверки. Личный чат стажёра приватен; сервер не доверяет role/internId клиента.'},'servers':[{'url':'/api/v1','description':'Проект Go API, отсутствующий в текущем макете'}],'paths':P,'components':{'securitySchemes':{'sessionCookie':{'type':'apiKey','in':'cookie','name':'homych_session','description':'Схематичная проверенная сервером сессия. SSO/transport по OQ02. При cookie-auth мутации требуют проверки Origin и CSRF-токена; конкретный handshake согласовать до реализации.'}},'schemas':SC},'x-status':'proposed-not-implemented','x-date':'2026-11-12'}
    old.write_text(json.dumps(doc,ensure_ascii=False,indent=2)+'\n','utf-8')
    print(f'{sum(len(x) for x in P.values())} proposed API operations; {len(SC)} schemas')

if __name__=='__main__':
    import re
    main()
