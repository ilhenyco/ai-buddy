"""Package editable diagrams with the current traceability and readme."""
from pathlib import Path
from zipfile import ZipFile,ZIP_DEFLATED
import hashlib
import json

BASE=Path(__file__).parent
ROOT=BASE.parents[2]
out=ROOT/'output'
models=out/'bpmn-v3/models'
images=out/'bpmn-v3/images'
index=json.loads((models/'model-index.json').read_text('utf-8'))
report=json.loads((images/'render-report.json').read_text('utf-8'))
assert len(index['models'])==len(report)==18
assert all(x['editAndReimport'] and x['importWarnings']==0 for x in report)
readme='''# Хомыч — BPMN версии 3

Редакция от 11 ноября 2026 года по функциональным требованиям 0.2.

## Как открыть и редактировать

В папке models находятся 18 файлов P00.bpmn–P17.bpmn. Импортируйте нужный файл
в редактор BPMN, например https://demo.bpmn.io/ . Редактируйте блоки, дорожки
и стрелки и сохраняйте результат в BPMN. PNG и SVG предназначены для просмотра;
для изменения логики используйте исходный .bpmn.

В images лежат отдельные изображения каждой схемы. SVG можно увеличивать.
Функциональные требования приложены в functional-requirements.md.
models/requirements-traceability.csv связывает 42 требования с узлами,
models/traceability.csv сохраняет связь с 40 строками исторического чек-листа.
CSV сохранены в UTF-8 с BOM и разделителем «;».

## Что изменено

Периоды запускаются по календарю и не зависят от галочек. Руководитель собирает
сценарий из блоков. Стажёр, бадди и руководитель получают разные сообщения одного
периода. Большинство действий выполняется по внешним ссылкам. Результаты встреч
остаются вне продукта. P15 раскрывает рассылку, P16 — доставку, P17 — предложение
по обновлению RAG. Подробности — в CHANGELOG.md.

P00, P15 и P16 описывают разные уровни одного механизма. Они не запускают
три независимые рассылки. Человеческие процессы не возвращают условие завершения
календарю. Конечное событие рассылки не доказывает выполнение работы человеком.

Прямая интеграция P05–P06 и защищённые материалы P07 условны по OQ08.
P16 и P17 содержат предложения и не закрывают выбор канала или владельца RAG.
Пересылка личных AI ответов остаётся OQ01. Статусы требований сохранены в таблице.

## Проверки

Проверены XSD BPMN, связи, развилки и происхождение шагов. Все 18 моделей
открыты в BPMN Modeler, изменены, сохранены и повторно импортированы без
предупреждений. В checks сохранены отчёты. Проверки схем не заменяют тесты
приложения. Модели неисполняемые; выбор workflow engine не предполагается.

Версия 2 сохранена в проекте отдельно. Прежние экраны, API и T1–T5 требуют
актуализации. По совпадению ID нельзя автоматически переносить смысл старой задачи.
'''
files={}
for p in models.iterdir():
    if p.is_file():files['models/'+p.name]=p.read_bytes()
for p in images.iterdir():
    if p.suffix in ('.svg','.png'):files['images/'+p.name]=p.read_bytes()
files['README.md']=readme.encode('utf-8')
files['CHANGELOG.md']=(BASE/'CHANGELOG.md').read_bytes()
files['functional-requirements.md']=(BASE.parent/'functional-requirements.md').read_bytes()
files['checks/render-report.json']=(images/'render-report.json').read_bytes()
files['checks/semantic-checks.json']=(models/'semantic-checks.json').read_bytes()
files['checks/file-hashes.json']=json.dumps({n:hashlib.sha256(b).hexdigest() for n,b in files.items()},indent=2).encode('utf-8')
archive=out/'ai-buddy-bpmn-v3.zip'
with ZipFile(archive,'w',ZIP_DEFLATED) as z:
    for name,data in sorted(files.items()):z.writestr(name,data)
with ZipFile(archive) as z:
    assert z.testzip() is None
    assert len([p for p in z.namelist() if p.endswith('.bpmn')])==18
    assert all(z.read(n)==b for n,b in files.items())
print(json.dumps({'archive':str(archive),'entries':len(files),'models':18,'bytes':archive.stat().st_size},ensure_ascii=False))
