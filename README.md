# AI-buddy — Хомыч

Бот от команды Origination для Avito Hack&Change. Концепция: стажёр получает сообщения-чек-листы по календарю и задаёт личные вопросы; бадди и руководитель получают отдельные действия помощи. Отметки не блокируют этапы. Актуально: **13 ноября 2026**.

**Текущая интеграция:** основной FRONT связан с Go для входа и сценариев. Python RAG/MCP использует Go для проверки назначений, но ответы в основном чате ещё демонстрационные. Для живого RAG есть отдельная тестовая страница на `8091`, включаемая профилем `rag-demo`. Код этой ветки — `codex/rag-mcp`; он ещё не слит в `main`.

## Начать здесь

1. [Полная спецификация в Word](output/ai-buddy-team-specification.docx) — самостоятельный документ для команды.
2. [Каноническая концепция](docs/ai-buddy/team-specification.md), [97 карточек функций](docs/ai-buddy/frontend-functional-specification.md), [матрица связей](docs/ai-buddy/traceability.csv).
3. [Подключение сервера](docs/ai-buddy/server-handoff.md), [OpenAPI — проект 23 операций](docs/ai-buddy/system-analysis/openapi.yaml).
4. [BPMN P00–P17](docs/ai-buddy/bpmn-v3/README.md), [редактируемые модели](output/bpmn-v3/models/), [изображения и увеличенные фрагменты](docs/ai-buddy/bpmn-current/images/).
5. [47 продуктовых требований 0.4](docs/ai-buddy/functional-requirements.md), [Q&A](docs/ai-buddy/avito-qa.md), [расшифровка](docs/ai-buddy/avito-qa-transcript.md).
6. [Backend: запуск, тестовые учётки, демо-сценарии, как добавить свой модуль](backend/README.md).
7. [FRONT: работа с сервером, общий клиент `api.ts`](frontend/README.md#работа-с-сервером-vite_apion).
8. [RAG/MCP: запуск и контракт для Go](knowledge/README.md), [аудит по всем 47 требованиям](docs/ai-buddy/implementation-audit-2026-11-13.md).

## Запуск всего проекта

Python RAG/MCP: [запуск, контракты и передача Go-разработчику](knowledge/README.md).
Это отдельный opt-in стек `docker-compose.rag.yml`; базовый запуск ниже не требует модели.

Нужен Docker. Из корня репозитория:

```sh
docker compose up --build
```

| Что | Адрес | Переменная для смены порта |
|---|---|---|
| Приложение: фронт + API через nginx | http://localhost:8088 | `HOMYCH_WEB_PORT` |
| API напрямую | http://localhost:8080 (проверка: `curl localhost:8080/healthz`) | `HOMYCH_API_PORT` |
| PostgreSQL с pgvector | localhost:55432 | `HOMYCH_DB_PORT` |

**Вход** — демо-учётки вместо SSO, экран «Кто входит?». Руководитель Алексей, бадди Даша, стажёры Саша, Маша, Илья; вторая команда для проверки изоляции — руководитель Ирина, бадди Олег, стажёр Кирилл; HR Анна.

**Демо-данные** создаются при первом запуске: опубликованный сценарий Саши (в ленте сразу два сообщения), черновик Маши и сценарий Кирилла у Ирины — [подробно](backend/README.md#демо-сценарии). Начать с чистого листа: `docker compose down -v`, затем снова `docker compose up --build`.

Порядок показа: войти Алексеем → «Сообщения» (лента «Помощь стажёру» по плану Саши) → «Сценарий» → создать или открыть сценарий, выбрать стажёра, «Опубликовать».

Тесты backend: `cd backend && make db && make test`. Подробно — [backend/README.md](backend/README.md).

## Запуск RAG с живой моделью и тестовой страницей

Нужен работающий Docker с Compose. Команды первого запуска после клонирования:

```sh
git clone https://github.com/ilhenyco/ai-buddy.git
cd ai-buddy
git switch codex/rag-mcp
```

Создать `.env` из `.env.example`: в PowerShell `Copy-Item .env.example .env`, в bash `cp .env.example .env`. Это шаг для нового клона; существующий `.env` сохраняется. Заменить `RAG_SERVICE_TOKEN` случайной строкой не короче 24 символов. Один и тот же внутренний credential автоматически передаётся Go и Python; `.env` не коммитится. Значения тайм-аутов в примере рассчитаны на локальную пробу на CPU, не являются SLA.

```sh
docker compose -f docker-compose.yml -f docker-compose.rag.yml --profile rag-demo up -d --build --wait
docker compose -f docker-compose.yml -f docker-compose.rag.yml exec ollama ollama pull bge-m3:567m
docker compose -f docker-compose.yml -f docker-compose.rag.yml exec ollama ollama pull qwen3:4b
docker compose -f docker-compose.yml -f docker-compose.rag.yml exec knowledge python -m homych_knowledge.ingest playbook
```

Модели и первая индексация требуют времени и нескольких ГБ свободного диска. До успешной индексации `/readyz` возвращает 503 — запущенный процесс ещё не означает готовую базу знаний.

| Что открыть | Адрес | Что проверяем |
|---|---|---|
| Отдельная страница вопросов к RAG | http://127.0.0.1:8091/ | Реальная генерация и ссылки на Playbook; демо-стажёр `u-sasha` |
| Готовность индекса | http://localhost:8090/readyz | После ingestion — `200`, `status=ok` |
| Основное приложение | http://localhost:8088/ | Вход и сценарии через Go; основной AI-чат пока локальный |
| MCP | http://localhost:8090/mcp/ | Протокольный endpoint, требуется отдельный MCP credential; это не веб-страница |

Проверочные вопросы: «Что такое Avito PaaS?», «Какие правила действуют при изменении чужого кода?», «Какие возможности обучения описаны в Playbook?». Корпус — публичный справочник, а не полная внутренняя HR-база; некоторые сведения исторические.

Логи и остановка (данные БД/модели в volumes сохраняются):

```sh
docker compose -f docker-compose.yml -f docker-compose.rag.yml --profile rag-demo logs --tail 100 knowledge api rag-demo
docker compose -f docker-compose.yml -f docker-compose.rag.yml --profile rag-demo down
```

Повторный запуск — первая команда `up`; повторно скачивать уже сохранённые модели и индексировать неизменный корпус не нужно. Профиль `rag-demo` — локальный операторский стенд, не интерфейс с пользовательской авторизацией; его порт привязан к loopback. Это замена прежней локальной страницы из `tmp`, теперь она входит в Git и Compose.

Подробные варианты запуска без модели и native Python, MCP-авторизация и контракт интеграции — в [knowledge/README.md](knowledge/README.md).

## Что нужно соединить для основного чата

```text
Уже работает: React → Go (вход, сценарии)
Уже работает: Python RAG → Go (проверка текущих назначений)
Уже работает: тестовая страница → Python RAG → PostgreSQL/Ollama
Предстоит:   React → Go askQuestion/AnswerJob → Python RAG → Go история/getAnswer → React
```

Go должен сохранять вопросы и задания, повторять обработку после сбоя и возвращать статус/ответ через публичный API. FRONT должен перейти с `demoAnswer` на этот API и восстанавливать ожидание ответа после reload. В Python передаётся проверенный серверный контекст стажёра/сообщения. Контракты подготовлены, но сквозная интеграция ещё не реализована и требует E2E-проверки прав, повторов, тайм-аутов и источников.

## Запуск FRONT без сервера

Node **22.18+**, npm. После клонирования:

```sh
cd frontend
npm ci
npm run dev -- --host 127.0.0.1 --port 4173
```

Открыть http://127.0.0.1:4173 . Так фронт работает без сервера, все данные в браузере; для работы с сервером — `VITE_API=on`, см. [frontend/README.md](frontend/README.md#работа-с-сервером-vite_apion). Внешний ключ не нужен. Роли, стажёры и дата переключаются в панели проверки макета. Для списка сценариев выбрать руководителя и вкладку «Сценарий». Данные — browser localStorage `homych.frontend.bot.v3`; сброс удаляет локальную демонстрацию.

```sh
npm run check
npm test
npm run build
npm run test:sites
```

Сборка — `frontend/dist/client`; worker сохранён для существующей упаковки Sites. [Инструкция FRONT](frontend/README.md). CI выполняет эти проверки и структурную проверку передачи.

## Статус разработки

| Часть | Операции контракта | Статус |
|---|---|---|
| Каркас backend, демо-вход, `sessionContext` | sessionContext | ✅ в `main` |
| Сценарии руководителя | listTemplates, listScenarios, createScenario, getScenario, saveDraft, publishScenario | ✅ в `main` |
| FRONT ↔ API: демо-вход, экран сценариев, весь проект в Docker, демо-сценарии | — | ✅ в `main` |
| Планировщик, лента, отметки, уведомления | listMessages, setCheck, listNotifications | ⏳ не начато — без него лента стажёра и бадди пустая |
| Подсказки вопросов | getSuggestions | ⏳ не начато |
| Публичные вопросы и ответы AI | askQuestion, getAnswer | ⏳ Go AnswerJob и подключение FRONT; Python API реализован отдельно |
| Источники знаний | listSources, createSource, getSource, updateSource, excludeSource, getSourceJob | ⏳ не начато |
| Python RAG и MCP поиска | внутренние search/answer, MCP search_knowledge, Go ai-context | Реализация в `knowledge/`; публичные askQuestion/getAnswer и FRONT ещё требуют подключения |
| Заявка на доступ (OQ08, HTTP-мок) | createAccessDraft, confirmAccess, getAccess, getProtectedTask | ⏳ не начато |

Новый модуль добавляется в `backend/internal/<модуль>/` по образцу сценариев — см. [backend/README.md](backend/README.md). Обновляйте эту таблицу в своём PR.

## Границы готовности

FRONT покрывает основные сценарии как прототип; вход и конструктор подключены к Go. **Backend в работе**: `sessionContext` и шесть операций сценариев дают **7 из 23 операций публичного контракта**, остаются 16. Планировщик, лента/отметки, подсказки, AnswerJob/история, публичные источники, уведомления и HTTP-мок доступов ещё не реализованы. Python RAG и MCP-поиск работают отдельно, с серверной проверкой назначений через Go; их наличие не добавляет публичных `askQuestion/getAnswer`. Лента, отметки, AI-ответы основного FRONT, материалы и заявка остаются browser/localStorage-демонстрацией. Демо-вход отсутствует в `openapi.yaml`. Панель HR не утверждает права HR. Известные дефекты: потеря cutoff после reload и общий `unavailable` для части вопросов без источника — см. аудит. Число тестов и покрытие существующего кода не равны готовности продукта.

Канонические данные: `team-specification.md`, `frontend-functions.json`, `system-analysis/openapi.yaml`, `bpmn-v3/processes.py`. Скрипты сборки в tools; [сборка и рендер Word](docs/ai-buddy/document-build.md). Структурная проверка: `python tools/verify_team_handoff.py`.

История сохранена: [прежний общий Word](output/ai-buddy-product-bpmn.docx), [прежние семь операций](docs/ai-buddy/system-analysis/openapi-legacy.yaml), старые схемы и T1–T5. Актуальная передача заменяет их в качестве основания новой разработки. Онлайн-лист представлен локальным снимком; более поздние изменения не подтверждены. PR в main не сливается автоматически. Доступы команде выдаёт владелец.
