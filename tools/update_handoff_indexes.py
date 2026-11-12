from pathlib import Path
R=Path(__file__).resolve().parents[1];D=R/'docs/ai-buddy'
def replace(path,pairs):
    s=path.read_text('utf-8')
    for old,new in pairs:s=s.replace(old,new)
    path.write_text(s,'utf-8')
def notice(path,text):
    s=path.read_text('utf-8')
    if text not in s:
        lines=s.splitlines();lines[1:1]=['',text,''];path.write_text('\n'.join(lines)+'\n','utf-8')
replace(D/'bpmn-v3/README.md',[
 ('Актуальная редакция от 11 ноября 2026 года. Основание — [функциональные требования 0.2]','Актуальная редакция 3.1 от 12 ноября 2026 года. Основание — [функциональные требования 0.4]'),
 ('Открытые вопросы OQ01–OQ08 соответствуют требованиям 0.2.','OQ01 закрыт: личный диалог приватен. Открытые OQ02–OQ08 соответствуют требованиям 0.4.'),
 ('../../../output/bpmn-v3/images/','../bpmn-current/images/'),('Связь с 42 требованиями','Связь с 47 требованиями'),
 ('Рендерер открывает каждую модель, меняет имя задачи, сохраняет и повторно импортирует её.','Предыдущая редакция проходила round-trip в bpmn-js; текущая проверяется XSD, связностью и семантическими инвариантами. Новый рендер строится непосредственно по BPMN DI; изменение XML в редакторе повторно не заявляется.'),
 ('Прежние S0–S4, OpenAPI и задания T1–T5 требуют отдельного согласования с новой границей продукта; этот набор схем их автоматически не переписывает.','Прежние S0–S4 и ранние T1–T5 исторические. Текущие экраны S01–S10, проект OpenAPI и порядок разработки приведены в team-specification.md и server-handoff.md.'),
 ('Руководитель, шаблон 6/9 месяцев, даты, адресаты и публикация','Список/черновик, 6/9 месяцев, даты, адресаты, autosave и замена active'),
 ('Права, поиск, источники и резервный ответ','Подсказки по этапу, приватный вопрос, права, поиск, источники и fallback')])
notice(D/'README.md','**Актуальная передача 12.11.2026:** [концепция](team-specification.md), [каталог всех функций](frontend-functional-specification.md), [серверная передача](server-handoff.md), [BPMN P00–P17](bpmn-v3/README.md). Описание ниже сохранено как историческое: обязательные стартовые карточки и AI-генерация плана заменены chat-first концепцией.')
for p in ('system-design.md','handoff.md','system-analysis/process-scope.md'):
    target=D/p;prefix='../' if '/' in p else ''
    notice(target,f'**Историческая редакция.** Для новой разработки использовать [{prefix}team-specification.md]({prefix}team-specification.md) и [{prefix}server-handoff.md]({prefix}server-handoff.md). Старый OpenAPI сохранён отдельно; текущий контракт — проект 23 операций. OQ01 закрыт, чек-листы не блокируют календарь.')
notice(D/'product-context.md','**Основной актуальный документ команды:** [team-specification.md](team-specification.md), 12.11.2026. [Полный каталог FRONT](frontend-functional-specification.md) и [server-handoff.md](server-handoff.md) объединяют последние решения. Дальнейший текст сохраняет исследовательскую историю и основания; при расхождении сверять статус и дату решения.')
replace(D/'functional-requirements.md',[
 ('Состав ближайшего MVP после этих уточнений требует отдельного выбора.','Актуальная граница локального FRONT и предложенный порядок рабочего MVP описаны в team-specification.md; промышленный объём требует отдельных решений.'),
 ('BPMN, экраны S0–S4 и семь операций прежнего OpenAPI описывают предыдущую границу MVP. Их требуется согласовать с этой редакцией перед реализацией новых функций.','BPMN P00–P17 актуализированы до редакции 3.1 под требования 0.4; текущие экраны S01–S10 и проект 23 операций описаны в team-specification.md и server-handoff.md. Семь операций сохранены в openapi-legacy.yaml как история.'),
 ('Открытый вопрос — поведение пока нельзя однозначно реализовать.','Открытый вопрос — поведение пока нельзя однозначно реализовать. Подробные карточки всех функций FRONT: frontend-functional-specification.md; матрица связей: traceability.csv.')])
notice(R/'frontend/design/process-map.md','**Актуально 12.11.2026:** P01 включает список/поиск/черновики/autosave/замену; P14 — стабильные подсказки этапа и приватный диалог. Точная модель — output/bpmn-v3/models, полный каталог — docs/ai-buddy/frontend-functional-specification.md. Старые ссылки на требования 0.2 ниже исторические.')
print('Updated entry points and historical notices')
