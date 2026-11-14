# AI-buddy — Хомыч

Бот от команды Origination для Avito Hack&Change. Стажёр получает сообщения-чек-листы по календарю и задаёт личные вопросы AI-наставнику, который отвечает по материалам команды со ссылками на источники; бадди и руководитель получают свои действия помощи. Отметки не блокируют этапы. Актуально: **14 ноября 2026**.

```text
React (frontend/) ──cookie+CSRF──> Go API (backend/) ──> PostgreSQL 16 + pgvector
                                      │  ├─ планировщик этапов, лента, отметки, уведомления
                                      │  ├─ вопросы: AnswerJob → worker → Python RAG → ответ с источниками
                                      │  ├─ материалы: версии → очередь индексации Python RAG
                                      │  └─ заявки на доступ → мок-сервис доступов (OQ08)
Python RAG/MCP (knowledge/) ──ai-context──> Go: права и назначения стажёра
                            └─> Ollama: BGE-M3 (поиск), Qwen3:4b (ответ); MCP search_knowledge для агентов
```

Все **23 операции контракта** реализованы в Go и проверяются тестом маршрутов по `openapi.yaml`; покрытие Go ~89 %.

## Начать здесь

1. [Backend: запуск, модули, тестовые учётки, демо-сценарии](backend/README.md).
2. [FRONT: работа с сервером, общий клиент `api.ts`](frontend/README.md#работа-с-сервером-vite_apion).
3. [RAG/MCP: устройство, запуск, внешние агенты](knowledge/README.md), [отдельный сервер знаний](knowledge/EXTERNAL.md).
4. [OpenAPI сервера](docs/ai-buddy/system-analysis/openapi.yaml), [подключение сервера](docs/ai-buddy/server-handoff.md), [аудит реализации на 14 ноября](docs/ai-buddy/implementation-audit-2026-11-14.md).
5. [Полная спецификация в Word](output/ai-buddy-team-specification.docx), [каноническая концепция](docs/ai-buddy/team-specification.md), [97 карточек функций](docs/ai-buddy/frontend-functional-specification.md), [матрица связей](docs/ai-buddy/traceability.csv).
6. [BPMN P00–P17](docs/ai-buddy/bpmn-v3/README.md), [47 продуктовых требований 0.4](docs/ai-buddy/functional-requirements.md), [Q&A](docs/ai-buddy/avito-qa.md).

## Запуск

Нужен Docker. Из корня репозитория.

### Без модели (быстро)

```sh
docker compose up --build
```

| Что | Адрес | Переменная для смены порта |
|---|---|---|
| Приложение: фронт + API через nginx | http://localhost:8088 | `HOMYCH_WEB_PORT` |
| API напрямую | http://localhost:8080 (`curl localhost:8080/healthz`) | `HOMYCH_API_PORT` |
| Мок сервиса доступов | http://localhost:8091 | `HOMYCH_MOCK_PORT` |
| PostgreSQL с pgvector | localhost:55432 | `HOMYCH_DB_PORT` |

Всё, кроме ответов AI, работает. На вопрос стажёра Хомыч честно отвечает, что поиск по материалам на этом стенде не подключён, — вопрос сохраняется.

### С AI: RAG и живая модель

Нужны две модели Ollama: `bge-m3:567m` (поиск, ~1,2 ГБ) и `qwen3:4b` (ответы, ~2,5 ГБ). Команды ниже вводить по одной: zsh на macOS по умолчанию не понимает комментарии `#` в интерактивной строке.

**1. Настройки.** `cp .env.example .env` (PowerShell: `Copy-Item .env.example .env`), затем в `.env`:

- `RAG_SERVICE_TOKEN` — любая случайная строка от 24 символов;
- для экрана «Материалы»: `python knowledge/examples/keygen.py`, «Client token» → `RAG_SOURCES_TOKEN`, «Server token_sha256» → в `RAG_EXTERNAL_CLIENTS` по образцу из `.env.example`.

**2. Модель — один из вариантов.**

*Вариант А — Ollama на компьютере (рекомендуется на Mac: работает на GPU, ответ за секунды).* Установить с [ollama.com](https://ollama.com) или `brew install ollama`, затем:

```sh
ollama serve
```

В другом терминале (если приложение Ollama уже запущено, `ollama serve` не нужен):

```sh
ollama pull bge-m3:567m
ollama pull qwen3:4b
echo 'RAG_OLLAMA_URL=http://host.docker.internal:11434' >> .env
```

Прерванная загрузка продолжается с того же места: повторить `ollama pull`.

*Вариант Б — Ollama в Docker.* Docker Desktop с памятью от 8 ГБ. Модели скачиваются после запуска стенда (шаг 3).

**3. Запуск и индексация Playbook** (индексация — только в первый раз):

```sh
docker compose -f docker-compose.yml -f docker-compose.rag.yml up -d --build --wait
```

Только для варианта Б:

```sh
docker compose -f docker-compose.yml -f docker-compose.rag.yml exec ollama ollama pull bge-m3:567m
docker compose -f docker-compose.yml -f docker-compose.rag.yml exec ollama ollama pull qwen3:4b
```

Для обоих вариантов:

```sh
docker compose -f docker-compose.yml -f docker-compose.rag.yml exec knowledge python -m homych_knowledge.ingest playbook
curl http://localhost:8090/readyz
```

Готовность — `200` от `/readyz`. Индексация Playbook заменяет весь корпус: выполнять до добавления материалов через экран. Для проверки без модели — `RAG_PROVIDER=deterministic` в `.env` и повторить индексацию: ответы будут помечены как тестовые.

**4. Проверка.** Войти Сашей и спросить «Что такое Avito PaaS?» — ответ со ссылкой на Playbook; «Сколько дней отгула в первый месяц?» — «нет подтверждённого ответа, уточни у бадди». Если что-то не так:

```sh
docker compose -f docker-compose.yml -f docker-compose.rag.yml logs --tail 50 api knowledge knowledge-worker
```

Строки `answer request=…` в логе `knowledge` показывают, что нашёл поиск и что решила модель (без текста вопросов).

На CPU ответ модели занимает до минуты: в чате Хомыч показывает «Ищу в материалах команды», ответ сохраняется на сервере и приходит, даже если закрыть вкладку. Тайм-ауты в `.env.example` рассчитаны на CPU.

Остановка: `docker compose -f docker-compose.yml -f docker-compose.rag.yml down` (данные и модели остаются в volumes). Отдельная операторская страница вопросов к RAG — профиль `rag-demo`, http://127.0.0.1:8092.

**Вход** — демо-учётки вместо SSO. Руководитель Алексей, бадди Даша, стажёры Саша, Маша, Илья; вторая команда для проверки изоляции — руководитель Ирина, бадди Олег, стажёр Кирилл; HR Анна. **Демо-данные** создаются при первом запуске: опубликованный сценарий Саши, черновик Маши, сценарий Кирилла — [подробно](backend/README.md#демо-сценарии).

### Порядок показа

1. Профиль → «Демо-стенд» → «Начать демо заново».
2. **Саша:** лента этапов с чек-листами, вкладка «План», вопрос Хомычу — ответ со ссылками на источники; «Инструкция по доступам» → заявка → через 20 с «Доступ одобрен», открылась первая задача.
3. **Даша:** колонка «Мои стажёры», те же этапы в формулировке «помоги стажёру», статус заявки Саши; личного чата Саши не видно.
4. **Алексей:** «Сценарий» → создать и опубликовать; «Материалы» → добавить памятку → «В поиске» через несколько секунд → Саша получает ответ по ней.
5. «Демо-стенд» → дата на неделю вперёд: всем трём ролям приходит новый этап и уведомление в колокольчике, подсказки вопросов меняются.

## Проверки

```sh
cd backend && make db && make test && make cover   # Go: тесты на PostgreSQL, -race, покрытие
cd frontend && npm ci && npm run check && npm test && npm run build
cd knowledge && uv sync --frozen && uv run ruff check . && uv run pytest   # TEST_DATABASE_URL с pgvector для интеграционных
```

CI (`.github/workflows`): `backend.yml`, `frontend.yml`, `knowledge.yml` (включая Compose smoke) и структурная проверка передачи `python tools/verify_team_handoff.py`.

## Статус

| Часть | Операции контракта | Статус |
|---|---|---|
| Вход, сессия, CSRF/Origin | sessionContext | ✅ |
| Сценарии руководителя | listTemplates, listScenarios, createScenario, getScenario, saveDraft, publishScenario | ✅ |
| Сообщения по календарю: планировщик, лента всех ролей, отметки, уведомления | listMessages, setCheck, listNotifications | ✅ |
| Подсказки вопросов текущего этапа | getSuggestions | ✅ |
| Вопросы к AI: AnswerJob, worker с повторами, ответ RAG с источниками, история на сервере | askQuestion, getAnswer | ✅ |
| Материалы команды: версии, индексация в RAG, исключение | listSources, createSource, getSource, updateSource, excludeSource, getSourceJob | ✅ |
| Заявка на доступ через мок-сервис (OQ08), защищённая первая задача | createAccessDraft, confirmAccess, getAccess, getProtectedTask | ✅ |
| Python RAG: гибридный поиск с ACL, Qwen3:4b, MCP `search_knowledge` | внутренний API | ✅ |
| FRONT: вкладка «План», колонка «Мои стажёры», колокольчик, демо-стенд | — | ✅ |

Новый модуль добавляется в `backend/internal/<модуль>/` по образцу существующих — см. [backend/README.md](backend/README.md). Обновляйте эту таблицу в своём PR.

## Границы готовности

Сделано для демо, а в продукте заменяется: демо-вход вместо SSO (OQ02), мок вместо сервиса доступов Авито (OQ08), веб-канал вместо пуша/мессенджера (OQ02), публичный Avito Playbook вместо внутренней базы знаний. Контакты и тексты сценариев условные. Не реализованы: событийная шина (outbox → Kafka) и метрики пользы, фича-флаги, переназначение бадди через интерфейс, HR-рассылки (предложения). Подробно, с проверкой по всем 47 требованиям, — [аудит на 14 ноября](docs/ai-buddy/implementation-audit-2026-11-14.md).

Канонические данные: `team-specification.md`, `frontend-functions.json`, `system-analysis/openapi.yaml`, `bpmn-v3/processes.py`. Скрипты сборки в tools; [сборка и рендер Word](docs/ai-buddy/document-build.md). История: [прежний общий Word](output/ai-buddy-product-bpmn.docx), [прежние семь операций](docs/ai-buddy/system-analysis/openapi-legacy.yaml).

## Подключение внешних агентов

Агенты Авито подключаются к базе знаний Хомыча по MCP (`search_knowledge`) или HTTP без нашего фронтенда и Go: [развёртывание, авторизация, пополнение базы](knowledge/EXTERNAL.md).
