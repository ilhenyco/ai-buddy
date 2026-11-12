# AI-buddy — Хомыч

Бот от команды Origination для Avito Hack&Change. Стажёр получает сообщения-чек-листы по календарю и задаёт личные вопросы; бадди и руководитель получают отдельные действия помощи. Отметки не блокируют этапы. Актуально: **12 ноября 2026**.

## Начать здесь

1. [Полная спецификация в Word](output/ai-buddy-team-specification.docx) — самостоятельный документ для команды.
2. [Каноническая концепция](docs/ai-buddy/team-specification.md), [97 карточек функций](docs/ai-buddy/frontend-functional-specification.md), [матрица связей](docs/ai-buddy/traceability.csv).
3. [Подключение сервера](docs/ai-buddy/server-handoff.md), [OpenAPI — проект 23 операций](docs/ai-buddy/system-analysis/openapi.yaml).
4. [BPMN P00–P17](docs/ai-buddy/bpmn-v3/README.md), [редактируемые модели](output/bpmn-v3/models/), [изображения и увеличенные фрагменты](docs/ai-buddy/bpmn-current/images/).
5. [47 продуктовых требований 0.4](docs/ai-buddy/functional-requirements.md), [Q&A](docs/ai-buddy/avito-qa.md), [расшифровка](docs/ai-buddy/avito-qa-transcript.md).

## Запуск FRONT

Node **22.18+**, npm. После клонирования:

```sh
cd frontend
npm ci
npm run dev -- --host 127.0.0.1 --port 4173
```

Открыть http://127.0.0.1:4173 . Внешний ключ не нужен. Роли, стажёры и дата переключаются в панели проверки макета. Для списка сценариев выбрать руководителя и вкладку «Сценарий». Данные — browser localStorage `homych.frontend.bot.v3`; сброс удаляет локальную демонстрацию.

```sh
npm run check
npm test
npm run build
npm run test:sites
```

Сборка — `frontend/dist/client`; worker сохранён для существующей упаковки Sites. [Инструкция FRONT](frontend/README.md). CI выполняет эти проверки и структурную проверку передачи.

## Границы готовности

Готов локальный FRONT. **Backend отсутствует**: auth, PostgreSQL, scheduler, реальный RAG, Qwen/DeepSeek, MCP и HTTP-мок предстоят. Контакты и заявка условные. Контракт API — проект реализации. Панель HR не утверждает права HR. Количество тестов не является отчётом >80% покрытия.

Канонические данные: `team-specification.md`, `frontend-functions.json`, `system-analysis/openapi.yaml`, `bpmn-v3/processes.py`. Скрипты сборки в tools; [сборка и рендер Word](docs/ai-buddy/document-build.md). Структурная проверка: `python tools/verify_team_handoff.py`.

История сохранена: [прежний общий Word](output/ai-buddy-product-bpmn.docx), [прежние семь операций](docs/ai-buddy/system-analysis/openapi-legacy.yaml), старые схемы и T1–T5. Актуальная передача заменяет их в качестве основания новой разработки. Онлайн-лист представлен локальным снимком; более поздние изменения не подтверждены. PR в main не сливается автоматически. Доступы команде выдаёт владелец.
