# Документация Хомыча

Как запустить проект — в [корневом README](../README.md). Здесь собрано, что почитать о продукте и устройстве системы.

## С чего начать

| Кому | Что читать |
|---|---|
| Впервые знакомитесь с проектом | [Спецификация продукта](product/team-specification.md): продукт, роли, путь стажёра, границы MVP |
| Разработчику сервера | [OpenAPI](architecture/openapi.yaml) → [backend/README.md](../backend/README.md) → [инварианты данных](architecture/server-handoff.md) |
| Разработчику фронтенда | [Каталог из 97 функций](product/frontend-functional-specification.md) → [frontend/README.md](../frontend/README.md) |
| Интегратору AI / агентов Авито | [RAG и MCP](architecture/ai-rag-mcp.md) → [knowledge/README.md](../knowledge/README.md) → [внешний сервер знаний](../knowledge/EXTERNAL.md) |
| Проверяющему готовность | [Аудит реализации на 14 ноября](reports/implementation-audit-2026-11-14.md) → [отчёт о тестировании (PDF)](reports/homych-testing-report.pdf) |

## Продукт — `product/`

| Документ | Содержание |
|---|---|
| [team-specification.md](product/team-specification.md) | Основная спецификация: продукт, роли, сценарии, правила |
| [functional-requirements.md](product/functional-requirements.md) | 47 функциональных требований (версия 0.4) со статусами согласования |
| [frontend-functional-specification.md](product/frontend-functional-specification.md) | 97 карточек функций интерфейса; машиночитаемо — [frontend-functions.json](product/frontend-functions.json) |
| [traceability.csv](product/traceability.csv) | Связь функции ↔ требование ↔ экран ↔ BPMN ↔ API |
| [avito-qa.md](product/avito-qa.md) | 83 вопроса и ответа Авито с Q&A-сессии; [полная расшифровка](product/avito-qa-transcript.md) |
| [product-context.md](product/product-context.md) | Исходный контекст продукта и путь стажировки |
| [decisions/](product/decisions) | Принятые продуктовые решения: проактивный бот, библиотека сценариев, подсказки этапа |

## Архитектура — `architecture/`

| Документ | Содержание |
|---|---|
| [openapi.yaml](architecture/openapi.yaml) | Контракт API: 23 операции и 9 расширений (`x-extension`, `x-demo-only`); тест маршрутов в Go сверяется с ним |
| [events.md](architecture/events.md) | Бизнес-события (outbox): 14 событий, конверт, публикаторы, как считать метрики пользы |
| [server-handoff.md](architecture/server-handoff.md) | Обязательные инварианты данных и правила сервера |
| [ai-rag-mcp.md](architecture/ai-rag-mcp.md) | Техническая документация AI-части: поиск, генерация, MCP, развёртывание, требования к интеграции |
| [architecture-current.svg](architecture/architecture-current.svg) | Схема контейнеров и модулей |

## Процессы — `processes/`

| Документ | Содержание |
|---|---|
| [bpmn-v3/](processes/bpmn-v3/README.md) | Актуальные BPMN P00–P17 и скрипты их сборки; модели — в [output/bpmn-v3/models](../output/bpmn-v3/models) |
| [rendered/](processes/rendered) | PNG/SVG схем для просмотра |
| [bpmn-v2/](processes/bpmn-v2/README.md) | Предыдущая редакция; её код используется сборкой v3 |

## Отчёты — `reports/`

- [«Хомыч — отчёт о тестировании» (PDF)](reports/homych-testing-report.pdf): результаты 11 живых проверок, покрытие автотестами, качество RAG и устойчивость к сбоям. Замер на 16 ноября 2026 года, коммит `ad68387` (`main`); внутри приведены команды повторного замера. Исходный файл «Хомыч — отчёт о тестировании.pdf» сохранён без изменений под именем `homych-testing-report.pdf`; [копия для жюри](../output/homych-testing-report.pdf) находится в `output/`.
- [Аудит реализации на 14 ноября](reports/implementation-audit-2026-11-14.md): 47 требований и обязательства ТЗ — что готово и чем подтверждено.

## Архив — `archive/`

Исторические материалы. Сохранены для истории решений, для новой работы не используются: [первый индекс бизнес-анализа](archive/business-analysis-index.md), [прежняя системная спецификация](archive/system-design.md), [прежний план передачи](archive/handoff.md), [аудит на 13 ноября](archive/implementation-audit-2026-11-13.md), [прежний OpenAPI на 7 операций](archive/openapi-legacy.yaml), [BPMN первой редакции](archive/bpmn-v1), [интерактивные схемы](archive/ai-buddy-maps.html) и одноразовые скрипты переноса.

## Сборка документов

Word-спецификация, BPMN и рендеры собираются скриптами из [tools/](../tools/README.md). CI запускает `python tools/verify_team_handoff.py`: он проверяет связи функций, требований, API и BPMN и битые ссылки в README.
