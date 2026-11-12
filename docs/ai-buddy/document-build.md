# Сборка документа команды

Канонические источники: team-specification.md, frontend-functions.json, server-handoff.md, system-analysis/openapi.yaml, BPMN model-index и сохранённый checklist-source.json. Старый ai-buddy-product-bpmn.docx — шаблон оформления и историческая редакция, его байты не меняются.

1. `python tools/build_team_handoff.py` — карточки и Markdown. Авторский исходник карточек находится в этом скрипте; менять одновременно JSON вручную нельзя.
2. `python tools/build_handoff_contract.py` — проект OpenAPI. JSON-сериализация намеренно используется как корректный YAML 1.2.
3. `python docs/ai-buddy/bpmn-v3/build_models.py output/bpmn-v3/models PATH_TO_OMG_XSD` и `python docs/ai-buddy/bpmn-v3/verify_models.py output/bpmn-v3/models` — модели и трассировка. Нужны BPMN20.xsd и его стандартные зависимости OMG.
4. `python tools/render_handoff_bpmn.py`, `python tools/build_handoff_architecture.py` — изображения из BPMN DI и схема архитектуры. В Windows используется установленный Arial, в другом окружении нужно настроить путь шрифта.
5. `python tools/build_team_document.py` — Word и traceability.csv. Скрипт сохраняет Arial и A4/A3 геометрию прежнего документа. Крупные модели имеют обзор и увеличенные панели.
6. `python tools/verify_team_handoff.py` — целостность ID, API, файлов и ссылок; не замена визуальной и бизнес-проверке.

Зависимости сборки: python-docx, Pillow, lxml, reportlab, pypdfium2. В Codex применяется bundled runtime, определённый load_workspace_dependencies.

В этой Windows-среде packaged render_docx.py был запущен и вернул отсутствие LibreOffice/soffice. Резервный реальный рендер: отдельный невидимый экземпляр установленного Word открывает итоговый DOCX read-only, обновляет содержание, экспортирует PDF; pypdfium2 создаёт PNG всех страниц. Визуальная проверка выполняется по этим PNG. Промежуточные PDF/PNG хранятся в tmp/team-spec и не входят в поставку.

При следующих изменениях повторять экспорт и просмотр страниц; старый отчёт QA не удостоверяет новую версию. Все AT-UI — критерии, не заявление о выполненных автотестах. Предыдущие изображения/Word v3 могут описывать прошлую редакцию; актуальный атлас включён в ai-buddy-team-specification.docx.
