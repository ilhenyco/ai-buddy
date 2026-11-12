# AI-buddy — Хомыч

Бот от команды Origination для Avito Hack&Change. Стажёр получает сообщения-чек-листы по календарю и задаёт личные вопросы; бадди и руководитель получают отдельные действия помощи. Отметки не блокируют этапы. Актуально: **12 ноября 2026**.

## Начать здесь

1. [Полная спецификация в Word](output/ai-buddy-team-specification.docx) — самостоятельный документ для команды.
2. [Каноническая концепция](docs/ai-buddy/team-specification.md), [97 карточек функций](docs/ai-buddy/frontend-functional-specification.md), [матрица связей](docs/ai-buddy/traceability.csv).
3. [Подключение сервера](docs/ai-buddy/server-handoff.md), [OpenAPI — проект 23 операций](docs/ai-buddy/system-analysis/openapi.yaml).
4. [BPMN P00–P17](docs/ai-buddy/bpmn-v3/README.md), [редактируемые модели](output/bpmn-v3/models/), [изображения и увеличенные фрагменты](docs/ai-buddy/bpmn-current/images/).
5. [47 продуктовых требований 0.4](docs/ai-buddy/functional-requirements.md), [Q&A](docs/ai-buddy/avito-qa.md), [расшифровка](docs/ai-buddy/avito-qa-transcript.md).
6. [Backend: запуск, тестовые учётки, демо-сценарии, как добавить свой модуль](backend/README.md).
7. [FRONT: работа с сервером, общий клиент `api.ts`](frontend/README.md#работа-с-сервером-vite_apion).

## Запуск всего проекта

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
| Вопросы и ответы AI (RAG, модель) | askQuestion, getAnswer | ⏳ не начато |
| Источники знаний | listSources, createSource, getSource, updateSource, excludeSource, getSourceJob | ⏳ не начато |
| Заявка на доступ (OQ08, HTTP-мок), MCP | createAccessDraft, confirmAccess, getAccess, getProtectedTask | ⏳ не начато |

Новый модуль добавляется в `backend/internal/<модуль>/` по образцу сценариев — см. [backend/README.md](backend/README.md). Обновляйте эту таблицу в своём PR.

## Границы готовности

Готов FRONT. **Backend в работе**: готовы каркас на Go + PostgreSQL, демо-вход вместо SSO, `sessionContext` и модуль сценариев — 6 из 23 операций контракта (шаблоны, список, создание, черновик с проверкой версии, публикация с заменой и датой отсечки), тесты с покрытием ~90% и CI. Предстоят: планировщик и сообщения, отметки, подсказки, вопросы и ответы (RAG, Qwen/DeepSeek), источники, уведомления, HTTP-мок доступов и MCP. FRONT подключён к API для входа и сценариев руководителя; лента, отметки, ответы AI, материалы и заявка пока работают в браузере (localStorage) до появления своих модулей. Демо-вход отсутствует в `openapi.yaml` — добавить вместе с контрактом. Контакты и заявка условные. Контракт API — проект реализации. Панель HR не утверждает права HR. Количество тестов не является отчётом >80% покрытия.

Канонические данные: `team-specification.md`, `frontend-functions.json`, `system-analysis/openapi.yaml`, `bpmn-v3/processes.py`. Скрипты сборки в tools; [сборка и рендер Word](docs/ai-buddy/document-build.md). Структурная проверка: `python tools/verify_team_handoff.py`.

История сохранена: [прежний общий Word](output/ai-buddy-product-bpmn.docx), [прежние семь операций](docs/ai-buddy/system-analysis/openapi-legacy.yaml), старые схемы и T1–T5. Актуальная передача заменяет их в качестве основания новой разработки. Онлайн-лист представлен локальным снимком; более поздние изменения не подтверждены. PR в main не сливается автоматически. Доступы команде выдаёт владелец.
