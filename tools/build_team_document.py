"""Create the standalone Word from canonical repository materials.
Requires python-docx, Pillow, lxml. Rendering/export is a separate step.
"""
from pathlib import Path
import json, re, csv, hashlib, math
from copy import deepcopy
from PIL import Image
from docx import Document
from docx.shared import Inches,Pt,RGBColor
from docx.enum.section import WD_SECTION,WD_ORIENT
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from docx.enum.table import WD_TABLE_ALIGNMENT,WD_CELL_VERTICAL_ALIGNMENT
from docx.opc.constants import RELATIONSHIP_TYPE as RT
R=Path(__file__).resolve().parents[1];D=R/'docs/ai-buddy';O=R/'output';T=R/'tmp/team-spec'
F=json.loads((D/'frontend-functions.json').read_text('utf-8'))['features']
API=json.loads((D/'system-analysis/openapi.yaml').read_text('utf-8'))
MODELS=json.loads((O/'bpmn-v3/models/model-index.json').read_text('utf-8'))
RENDERS={m['id']:m for m in json.loads((D/'bpmn-current/render-index.json').read_text('utf-8'))}
OP={op['operationId']:(method.upper(),path,op) for path,methods in API['paths'].items() for method,op in methods.items()}
SOURCE=O/'ai-buddy-product-bpmn.docx'
TARGET=O/'ai-buddy-team-specification.docx'

def element(tag,**attrs):
    e=OxmlElement('w:'+tag)
    for k,v in attrs.items():e.set(qn('w:'+k),str(v))
    return e

def init():
    doc=Document(SOURCE)
    first_geometry=deepcopy(doc.sections[0]._sectPr)
    body=doc._element.body
    for child in list(body):
        body.remove(child)
    body.append(first_geometry)
    # Removed reference body must not carry its obsolete image parts into output.
    for rid,rel in list(doc.part.rels.items()):
        if rel.reltype==RT.IMAGE:doc.part.drop_rel(rid)
    for style in doc.styles:
        if style.type==1:
            style.font.name='Arial'
            if style.name in ('Title','Subtitle','Normal','Header','Footer') or style.name.startswith('Heading'):
                style.font.color.rgb=RGBColor(0,0,0)
            if style._element.pPr is not None:
                for border in list(style._element.pPr.findall(qn('w:pBdr'))):style._element.pPr.remove(border)
    for name,size in [('Normal',10.5),('Title',30),('Subtitle',12),('Heading 1',18),('Heading 2',13),('Heading 3',11)]:
        doc.styles[name].font.size=Pt(size)
    doc.styles['Normal'].paragraph_format.line_spacing=1.1
    doc.styles['Normal'].paragraph_format.space_after=Pt(6)
    for name in ('Heading 1','Heading 2','Heading 3'):
        doc.styles[name].paragraph_format.keep_with_next=True
    sec=doc.sections[0];sec.page_width=Inches(8.27);sec.page_height=Inches(11.69);sec.orientation=WD_ORIENT.PORTRAIT
    cols=sec._sectPr.find(qn('w:cols'))
    if cols is not None:sec._sectPr.remove(cols)
    sec._sectPr.append(element('cols',num=1,space=720))
    sec.left_margin=sec.right_margin=Inches(.79);sec.top_margin=sec.bottom_margin=Inches(.65)
    # New standalone header/footer in the reference's typography.
    for part in (sec.header,sec.footer):
        for p in part.paragraphs:p.clear()
    sec.header.paragraphs[0].add_run('Хомыч · Спецификация команды · 1.0 · 12.11.2026').font.size=Pt(8.5)
    foot=sec.footer.paragraphs[0];foot.alignment=WD_ALIGN_PARAGRAPH.RIGHT
    foot.add_run('Страница ')
    fld=element('fldSimple',instr='PAGE');foot._p.append(fld)
    return doc

def para(doc,text,style=None):
    p=doc.add_paragraph(style=style)
    # Native text and emphasis; repository links are labels in the standalone file.
    text=re.sub(r'\[([^]]+)\]\(([^)]+)\)',lambda m:m.group(1)+' ('+m.group(2)+')',text)
    for i,t in enumerate(re.split(r'(\*\*[^*]+\*\*|`[^`]+`)',text)):
        if t.startswith('**'):p.add_run(t[2:-2]).bold=True
        elif t.startswith('`'):p.add_run(t[1:-1])
        else:p.add_run(t)
    return p

def md(doc,path,skiptitle=True,level=1):
    block=[];code=False
    def flush():
        if block:para(doc,' '.join(block));block.clear()
    for line in path.read_text('utf-8').splitlines():
        if line.startswith('```'):flush();code=not code;continue
        if code:codepara(doc,line);continue
        if not line.strip():flush();continue
        if line.startswith('# '):
            flush()
            if not skiptitle:doc.add_heading(line[2:],0)
        elif line.startswith('## '):flush();doc.add_heading(line[3:],level)
        elif line.startswith('### '):flush();doc.add_heading(line[4:],min(level+1,3))
        elif line.startswith('- '):flush();para(doc,line[2:],'List Bullet')
        elif re.match(r'^\d+\. ',line):flush();para(doc,re.sub(r'^\d+\. ','',line),'List Number')
        else:block.append(line)
    flush()

def codepara(doc,text):
    p=doc.add_paragraph();p.paragraph_format.space_after=Pt(0);p.paragraph_format.line_spacing=1.0
    p.paragraph_format.keep_with_next=False
    r=p.add_run(text);r.font.name='Consolas';r.font.size=Pt(9)
    return p

def table(doc,headers,rows,widths,font=9.5):
    tab=doc.add_table(rows=1,cols=len(headers));tab.alignment=WD_TABLE_ALIGNMENT.CENTER;tab.autofit=False
    for c,w in zip(tab.columns,widths):c.width=Inches(w)
    head=tab.rows[0];head._tr.get_or_add_trPr().append(element('tblHeader'))
    for i,h in enumerate(headers):head.cells[i].text=h
    for row in rows:
        cells=tab.add_row().cells
        for i,value in enumerate(row):cells[i].text=str(value)
    for ri,row in enumerate(tab.rows):
        row._tr.get_or_add_trPr().append(element('cantSplit'))
        for ci,cell in enumerate(row.cells):
            cell.width=Inches(widths[ci]);cell.vertical_alignment=WD_CELL_VERTICAL_ALIGNMENT.CENTER
            pr=cell._tc.get_or_add_tcPr();pr.append(element('shd',fill='E8EEF4' if ri==0 else ('F7F9FB' if ri%2==0 else 'FFFFFF')))
            borders=element('tcBorders')
            for side in ('top','left','bottom','right'):borders.append(element(side,val='single',sz=4,color='D9D9D9'))
            pr.append(borders);marg=element('tcMar')
            for side in ('top','bottom'):marg.append(element(side,w=65,type='dxa'))
            for side in ('left','right'):marg.append(element(side,w=85,type='dxa'))
            pr.append(marg)
            for p in cell.paragraphs:
                p.paragraph_format.space_after=Pt(3);p.paragraph_format.space_before=Pt(2);p.paragraph_format.line_spacing=1.05
                for run in p.runs:run.font.size=Pt(font);run.font.bold=ri==0;run.font.color.rgb=RGBColor(0,0,0)
    doc.add_paragraph().paragraph_format.space_after=Pt(2)
    return tab

def picture(doc,path,maxw=6.65,maxh=8):
    with Image.open(path) as im:w,h=im.size
    scale=min(maxw/w,maxh/h);p=doc.add_paragraph();p.alignment=WD_ALIGN_PARAGRAPH.CENTER
    p.add_run().add_picture(str(path),width=Inches(w*scale));return p

def datatype(schema):
    if '$ref' in schema:return schema['$ref'].rsplit('/',1)[-1]
    if 'anyOf' in schema:return '/'.join(datatype(x) for x in schema['anyOf'])
    if 'enum' in schema:return ', '.join(map(str,schema['enum']))
    if 'const' in schema:return str(schema['const'])
    t=schema.get('type','object')
    if t=='array':return 'список '+datatype(schema['items'])
    if isinstance(t,list):return '/'.join(t)
    return t+(' · '+schema['format'] if 'format' in schema else '')

def io(c):
    if 'inputs' in c:return c['inputs'],c['outputs']
    inputs=[];outputs=[]
    for oid in c['api']:
        if oid not in OP:continue
        _,_,op=OP[oid]
        body=op.get('requestBody',{}).get('content',{}).get('application/json',{}).get('schema',{})
        if '$ref' in body:
            n=body['$ref'].rsplit('/',1)[-1];inputs.append(n+': '+', '.join(API['components']['schemas'][n].get('properties',{})))
        for code,r in op['responses'].items():
            if code.startswith('2'):
                out=r.get('content',{}).get('application/json',{}).get('schema',{});outputs.append(datatype(out))
    if not inputs:
        if any(s in c['screens'] for s in ('S05','S06')):inputs=['scenarioId, draft и выбранный элемент/действие редактора']
        elif 'S07' in c['screens']:inputs=['текст поиска или sourceId/название/текст/выбранный TXT/MD']
        elif 'S10' in c['screens']:inputs=['resourceId, reason, подтверждение пользователя или operationId']
        elif 'S09' in c['screens']:inputs=['выбранные роль/синтетический профиль/дата/состояние проверки']
        else:inputs=['текущий профиль, роль, активная публикация и действие на элементе переписки']
    return '; '.join(dict.fromkeys(inputs)), ', '.join(dict.fromkeys(outputs)) or 'обновлённый интерфейс/локальное состояние по описанному действию'

def trace(cards):
    path=D/'traceability.csv'
    with path.open('w',encoding='utf-8-sig',newline='') as f:
        w=csv.writer(f,delimiter=';');w.writerow(['Функция','Требование / продуктовый FR','Экран','BPMN','API operationId (проект)','Данные','Проверка / статус'])
        for c in cards:
            inputs,outputs=io(c);w.writerow([c['id']+' '+c['name'],c['requirement']+' / '+c['parent'],', '.join(c['screens']),', '.join(c['bpmn']),', '.join(c['api']) or 'Локальная/демо операция',inputs+' → '+outputs,c['check']+' — критерий; UI/server тест ещё не заявлен'])

def build():
    T.mkdir(parents=True,exist_ok=True);trace(F)
    sha=hashlib.sha256(SOURCE.read_bytes()).hexdigest()
    (T/'artifact.md').write_text(f'''# Artifact record
Reference: {SOURCE.name}
SHA256: {sha}
Evidence: reference OOXML Arial Normal 10.5 pt, Title 30, H1 18, H2 13; A4 portrait margins .79in and A3 landscape .63in. Reference exported through installed Word to reference.pdf for inspection.
Allowed changes: replace product narrative, diagrams, cards and appendices with current canonical materials; retain typography and A4/A3 geometry. A3 is one column instead of the historical two-column appendix so wide figures remain readable. New output, historical file unchanged. Remove obsolete header/footer wording. Large BPMN have a complete overview and numbered overlapping details.
Source files: team-specification.md, frontend-functions.json, openapi.yaml, server-handoff.md, current BPMN model-index, original checklist snapshot and labelled UI screenshots.
Rendering: packaged render_docx.py diagnosed missing LibreOffice; actual Windows Word PDF export + pypdfium2 raster QA used. No claim of successful LibreOffice rendering.
''','utf-8')
    doc=init();doc.core_properties.title='Хомыч Спецификация продукта для команды разработки';doc.core_properties.subject='FRONT, процессы, требования и проект сервера';doc.core_properties.author='Команда Origination';doc.core_properties.version='1.0'
    para(doc,'Хомыч\nСпецификация продукта для команды разработки','Title')
    para(doc,'Полный контекст · FRONT · процессы · требования · проект сервера','Subtitle')
    para(doc,'Версия 1.0 · 12 ноября 2026 года')
    para(doc,f'Для команды разработки. {len(F)} функциональных карточек, 18 BPMN P00–P17, 40 строк исходного чек-листа и 23 проектные операции API.')
    para(doc,'Читать без переписки: сначала концепция и роли, затем экраны и функции. В приложениях — все процессы, контракты и матрица передачи. Статусы «макет», «демо», «согласовано», «предложение» и «проект сервера» различаются явно.')
    para(doc,'Важно: документ описывает действующий локальный FRONT и проект серверной части. Реальная авторизация, RAG, модель, MCP и корпоративные сервисы ещё не реализованы. Прежний общий документ сохранён как история.')
    doc.add_page_break();toc_title=doc.add_paragraph('Содержание',style='Title')
    for run in toc_title.runs:run.font.size=Pt(18)
    p=doc.add_paragraph();fld=element('fldSimple',instr='TOC \\o "1-1" \\h \\z \\u');p._p.append(fld)
    doc.add_page_break();md(doc,D/'team-specification.md')
    doc.add_page_break();doc.add_heading('13. Экраны прототипа с пояснениями',1)
    screens=[('S01 · Сообщения и вопросы этапа','suggestions-start.png','Снимок текущей редакции подсказок: три кнопки, единая лента и поле ввода. Сообщение этапа инициируется календарём, а не выбором этапа в интерфейсе.'),
      ('S02 · Сообщение бадди','friends-buddy.png','Снимок базового chat-first оформления: отдельные действия помощи по пунктам стажёра. Личный AI-чат стажёра помощнику не показывается.'),
      ('S04 · Библиотека сценариев','scenario-library-desktop.png','Снимок актуальной библиотеки: персональные варианты, фильтры, статусы, продолжение draft.'),
      ('S05 · Настройка черновика','scenario-builder-draft.png','Снимок редактора после добавления списка сценариев: возврат, название, параметры, блоки и сохранение.'),
      ('S06 · Замена варианта','scenario-replacement.png','Подтверждение сообщает, какой активный вариант заменяется, и сохраняет старую историю.'),
      ('S07 · Материалы','knowledge-desktop.png','Снимок предложенного интерфейса источников. Роль владельца, ACL и реальная индексация ещё требуют реализации.'),
      ('S01 · Подсказки на узком экране','suggestions-mobile.png','Кнопки остаются в переписке и прокручиваются горизонтально; это не меню навигации по этапам.')]
    for index,(title,img,caption) in enumerate(screens):
        if index:doc.add_page_break()
        doc.add_heading(title,2);para(doc,caption).paragraph_format.keep_with_next=True;picture(doc,R/'frontend/design/screenshots'/img,6.65,7.8)
    arch=D/'architecture-current.png'
    if arch.exists():
        doc.add_page_break();doc.add_heading('Архитектура: границы ответственности',2);picture(doc,arch,6.65,5.0);para(doc,'Схема проектная: во FRONT сейчас работает только левый компонент и локальные имитации. Сетевые стрелки показывают будущие взаимодействия. Инструмент вызывается Go API после подтверждения человека, а не календарным worker. В альбомном приложении схема повторена крупнее.')
    doc.add_page_break();doc.add_heading('14. Полный каталог функций и требований',1)
    para(doc,'Каждая карточка имеет собственный FR-UI и критерий AT. Указанные проверки — инструкция команде; наличие карточки не означает готовый автоматический UI-тест. Серверные контракты проектные. Условия начала: открыт указанный экран и существует нужный локальный профиль; для будущего сервера — проверенная сессия и назначение.')
    for c in F:
        doc.add_heading(c['requirement']+' · '+c['name'],2)
        para(doc,f"**Статус:** {c['status']}. **Участники:** {c['actor']}. **Начало:** {', '.join(c['screens'])}; контекст {', '.join(c['bpmn'])}.")
        para(doc,'**Действия и реакция:** '+c['behaviour'])
        inputs,outputs=io(c);para(doc,'**Вход:** '+inputs+'. **Выход:** '+outputs+'.')
        para(doc,'**Сохранение, ошибки и ограничения:** '+c['limits'])
        para(doc,f"**Приёмка {c['check']}:** выполнить описанное действие и проверить конкретный ожидаемый результат и ограничение. Для сохранённых данных повторить после перезагрузки. Связь: {c['parent']}; API: {', '.join(c['api']) or 'локально/демо, отдельный вызов не нужен'}.")
    doc.add_heading('15. Передача серверной команде',1);md(doc,D/'server-handoff.md',level=2)
    doc.add_heading('16. Проект API: операции, примеры и ошибки',1)
    para(doc,'Все операции имеют базовый путь /api/v1 и проверенную сессию. Это проект, а не описание существующего сервера. В JSON ниже синтетические примеры формы обмена, не реальные данные. Для GET вход — путь/параметры; для мутаций — тело и обязательные заголовки. Общие ошибки перечислены в конце раздела и полностью представлены в машиночитаемом контракте.')
    for oid,(method,path,op) in OP.items():
        doc.add_heading(oid+' · '+method+' '+path,2);para(doc,op['summary']+'. '+op['description'])
        if op['parameters']:para(doc,'**Параметры:** '+'; '.join(p['name']+' ('+p['in']+', '+('обязателен' if p.get('required') else 'необязателен')+')' for p in op['parameters']))
        body=op.get('requestBody',{}).get('content',{}).get('application/json')
        if body:
            para(doc,'**Пример запроса:**')
            for line in json.dumps(body['example'],ensure_ascii=False,indent=2).splitlines():codepara(doc,line)
        code=next(c for c in op['responses'] if c.startswith('2'));response=op['responses'][code]
        para(doc,'**Ответ '+code+' · '+datatype(response['content']['application/json']['schema'])+' (пример):**')
        for line in json.dumps(response['content']['application/json']['example'],ensure_ascii=False,indent=2).splitlines():codepara(doc,line)
        para(doc,'**Ошибки:** 401, 403, 404, 409, 412, 422, 429, 503 по общей модели Error; применимость зависит от действия. Ввод и ключ текущей попытки сохранять при повторе.')
    para(doc,'**Пример ошибки конкурентной правки:**')
    for line in json.dumps(API['components']['schemas']['Error']['example'],ensure_ascii=False,indent=2).splitlines():codepara(doc,line)
    doc.add_heading('17. Словарь полей API и серверные состояния',1)
    para(doc,'Обязательность относится к сериализации контракта. Пустые строки и null в draft допустимы по отдельным полям; publish дополнительно проверяет бизнес-полноту. Названия пользователей в локальном UI должны преобразовываться через справочник идентификаторов. Лимиты — предложения для проверки, не принятый SLA.')
    for name,schema in API['components']['schemas'].items():
        doc.add_heading(name,2)
        rows=[]
        for field,s in schema.get('properties',{}).items():rows.append([field,datatype(s),'Да' if field in schema.get('required',[]) else 'Нет'])
        table(doc,['Поле','Тип / допустимые значения','Обяз.'],rows,[1.7,4.1,.85])
    doc.add_heading('18. Все 40 шагов листа 3 и актуальные поправки',1)
    para(doc,'Далее сохранён исходный снимок каждого пункта со всеми шестью полями. Это исторический источник, а не автоматически действующая спецификация. Поправки: AI не обязан составлять план; checklist не управляет этапами; результаты/встречи вне продукта; личный чат приватен; HR и управление знаниями условны. В случае расхождения применять текущие требования, а не старое описание результата.')
    checklist=json.loads((D/'bpmn-v2/checklist-source.json').read_text('utf-8'))
    for i,row in enumerate(checklist[1:],2):
        doc.add_heading(f'Строка {i} · {row[0]}',2)
        for title,value in zip(checklist[0][1:],row[1:]):para(doc,'**'+title+':** '+value)
        nodes=MODELS['rows'][str(i)];para(doc,'**Актуальная привязка:** '+', '.join(nodes)+'. Применять описанные выше ограничения и актуальный процесс; упоминание AI в исходной строке само по себе не означает обязательный вызов модели.')
    doc.add_heading('19. Реестр процессов BPMN P00–P17',1)
    para(doc,'Нотация BPMN 2.0.2, неисполняемые модели с BPMN DI. Круг — событие; часы — календарное ожидание; скруглённая задача — действие; ромб с X — выбор одной ветви, с + — параллельность; сплошная стрелка — sequence flow; пунктир между пулами — message flow. Дорожки показывают ответственность, а не автоматически доступный UI.')
    for m in MODELS['models']:
        doc.add_heading(m['id']+' · '+m['title'],2)
        para(doc,'**Область:** '+m['scope']+'. **Начало:** '+m['trigger']+'. **Результат:** '+m['result']+'.')
        para(doc,'**Приёмка:** '+m['acceptance'])
        for note in m['notes']:para(doc,note)
        # The exact activity labels make the editable graph searchable in Word.
        para(doc,'**Узлы:** '+'; '.join(k+' — '+n['name'] for k,n in m['nodes'].items() if n['name'])+'.')
    sec=doc.add_section(WD_SECTION.NEW_PAGE);sec.orientation=WD_ORIENT.LANDSCAPE;sec.page_width=Inches(16.54);sec.page_height=Inches(11.69);sec.left_margin=sec.right_margin=Inches(.63);sec.top_margin=sec.bottom_margin=Inches(.55)
    cols=sec._sectPr.find(qn('w:cols'))
    if cols is not None:sec._sectPr.remove(cols)
    sec._sectPr.append(element('cols',num=1,space=720))
    doc.add_heading('20. Матрица передачи в разработку и проверку',1)
    para(doc,'API — будущий сервер. Данные/сохранение раскрыты в карточке с тем же ID. AT — критерий проверки, а не подтверждение выполненного автотеста. Демо-действия не требуют отдельного endpoint.')
    for start in range(0,len(F),18):
        if start:doc.add_page_break();doc.add_heading(f'Матрица · продолжение {start+1}–{min(start+18,len(F))}',2)
        rows=[]
        for c in F[start:start+18]:
            inputs,outputs=io(c);rows.append([c['id']+' '+c['name'],c['requirement']+'\n'+c['parent'],', '.join(c['screens']),', '.join(c['bpmn']),', '.join(c['api']) or 'Локально/демо',outputs,c['check']+'\nКритерий'])
        table(doc,['Функция','Требование','Экран','BPMN','API (проект)','Данные / выход','Проверка'],rows,[3.1,1.65,.65,.85,2.85,3.0,1.55],8.5)
    doc.add_page_break();doc.add_heading('Архитектура · увеличенная схема',2)
    picture(doc,D/'architecture-current.png',15.05,8.7)
    para(doc,'Проект серверной реализации. Существующий FRONT слева; остальные компоненты предстоит реализовать. MCP — опциональный адаптер инструментов Go API, не отдельный пользовательский интерфейс и не полномочия модели.')
    doc.add_page_break();doc.add_heading('21. Полный графический комплект BPMN',1)
    para(doc,'Каждый процесс показан полностью. У широких схем после общей карты идут увеличенные фрагменты слева направо с перекрытием. Общая карта нужна для навигации; читать подписи следует на увеличенных страницах. Переход через границу фрагмента — та же стрелка одной модели, а не новое событие. Редактируемые XML находятся в output/bpmn-v3/models/P00.bpmn … P17.bpmn.')
    first=True
    for m in MODELS['models']:
        r=RENDERS[m['id']]
        doc.add_page_break();doc.add_heading(m['id']+' · '+m['title'],2)
        para(doc,'Полная схема · слева направо. '+('Обзор: подписи увеличены на следующих страницах.' if r['panels'] else 'Читаемая полная модель.'))
        picture(doc,D/'bpmn-current/images'/r['image'],15.05,8.7)
        if not r['panels']:para(doc,m['acceptance'])
        for i,img in enumerate(r['panels'],1):
            doc.add_page_break();doc.add_heading(f"{m['id']} · увеличенный фрагмент {i}/{len(r['panels'])}",2)
            para(doc,'Продолжение общей модели слева направо. Дорожки по вертикали те же, что в полной схеме; соседние фрагменты перекрываются. Все переходы сохранены.')
            picture(doc,D/'bpmn-current/images'/img,15.05,8.7)
    doc.save(TARGET)
    assert hashlib.sha256(SOURCE.read_bytes()).hexdigest()==sha,'Reference changed'
    print(json.dumps({'output':str(TARGET),'features':len(F),'bpmn':len(MODELS['models']),'operations':len(OP),'reference_unchanged':True},ensure_ascii=False))

if __name__=='__main__':build()
