# RAG и MCP Хомыча

Python 3.12. HTTP и MCP используют общий retrieval с ACL. Go владеет идентичностью, назначениями
и сценариями; хранение истории ответов ещё требуется реализовать в Go. Модуль не добавляет публичные `askQuestion/getAnswer` и не переключает FRONT
с локальных AI-ответов. Инструкция следующему разработчику Go — ниже.

## Запуск

Из корня скопировать `.env.example` в `.env` и заменить `RAG_SERVICE_TOKEN` случайным значением
минимум 24 символа. Внутренний credential должен совпадать у Go и Python и не попадать в браузер.

```sh
docker compose -f docker-compose.yml -f docker-compose.rag.yml up -d --build
docker compose -f docker-compose.yml -f docker-compose.rag.yml exec ollama ollama pull bge-m3:567m
docker compose -f docker-compose.yml -f docker-compose.rag.yml exec ollama ollama pull qwen3:4b
docker compose -f docker-compose.yml -f docker-compose.rag.yml exec knowledge python -m homych_knowledge.ingest playbook
```

Для отдельной тестовой страницы добавить профиль (можно выполнить после предыдущих команд):

```sh
docker compose -f docker-compose.yml -f docker-compose.rag.yml --profile rag-demo up -d --build --wait
```

Открыть http://127.0.0.1:8091/ . Вся последовательность для нового клона приведена в [корневом README](../README.md#запуск-rag-с-живой-моделью-и-тестовой-страницей).
Страница запускается отдельным `rag-demo`-сервисом, фиксирует демо-пользователя на сервере и
не отдаёт браузеру service token. Она не проверяет пользовательскую сессию и предназначена только
для локального оператора в DEMO_MODE. Это не подключение основного чата React к RAG.

Сервис: `http://localhost:8090`. MCP: `http://localhost:8090/mcp/` с завершающим `/`.
`/healthz` проверяет процесс, `/readyz` — доступный опубликованный индекс, до ingestion возвращает
503. Это не проверка всех зависимостей. Compose использует CPU; для GPU нужен override под хост.
В native CPU-пробе на Ryzen 7 8845HS ответ занимал около 43 секунд; это отдельное наблюдение,
не SLA и не измерение p95/нагрузки. Потребление RAM/VRAM не оформлено как проверенный минимум.
Базовый `docker compose up` работает как раньше.

Пример внутреннего запроса, shell с экспортированной переменной:

```sh
curl http://localhost:8090/internal/v1/answer \
  -H "Authorization: Bearer $RAG_SERVICE_TOKEN" -H 'Content-Type: application/json' \
  -d '{"subjectId":"u-sasha","requestId":"demo-1","query":"Как устроено обучение и развитие?"}'
```

В PowerShell использовать `$env:RAG_SERVICE_TOKEN` и `curl.exe` либо `Invoke-RestMethod`.

### Native Python и локальный стенд

Нужны отдельно запущенные PostgreSQL с pgvector, Go API с тем же `RAG_SERVICE_TOKEN` и Ollama
с установленными моделями. Для них настроить `RAG_DATABASE_URL`, `RAG_GO_URL`, `RAG_OLLAMA_URL`
через окружение. Из `knowledge` установить зависимости `uv sync --frozen`.

На Windows PowerShell секрет и режим можно загрузить из корневого dotenv, затем запустить API:

```powershell
$env:RAG_DEMO_MODE = 'true'
uv run python -m dotenv -f ../.env run python -m homych_knowledge.app
```

После подготовки БД и моделей в другом терминале выполнить ingestion:

```powershell
$env:RAG_DEMO_MODE = 'true'
uv run python -m dotenv -f ../.env run python -m homych_knowledge.ingest playbook
```

Для страницы вопросов в третьем терминале:

```powershell
$env:RAG_DEMO_MODE = 'true'
uv run python -m homych_knowledge.demo --env-file ../.env
```

Native API по умолчанию слушает `8090`; демо-страница — `127.0.0.1:8091`.
Системные пути и установленные вручную зависимости конкретного Windows/WSL-стенда в Git не входят;
для воспроизводимого запуска команды используйте Compose. Чтобы не конфликтовать с уже работающим
native-стендом на этой машине, остановите его перед запуском тех же портов в Docker или задайте
`HOMYCH_API_PORT`, `HOMYCH_DB_PORT`, `HOMYCH_RAG_PORT`, `HOMYCH_DEMO_PORT`.

## MCP

В `.env` настроить отдельный token, связанный с существующим демо-стажёром, затем пересоздать
`knowledge`:

```dotenv
RAG_MCP_TOKENS={"replace-with-a-separate-random-mcp-token":"u-sasha"}
```

Клиент передаёт `Authorization: Bearer <MCP token>`. Без сопоставлений MCP запрещён.
Единственный tool `search_knowledge(query, limit=5)` возвращает до пяти фрагментов с источниками,
ревизиями, `snapshotId` и режимом поиска. Генерацию не запускает, внешние операции не выполняет.
`subjectId`, `teamId` и роль не являются аргументами инструмента: пользователь определяется token,
затем Go проверяет его назначения. Тексты источников — недоверенные данные, не команды агенту.

Это закрытый демо-адаптер, не OAuth и не проверенное подключение к платформе Авито. Корпоративную
авторизацию добавить по контракту платформы. SDK ограничивает Host/Origin loopback-адресами; для
другого hostname требуется явный ограниченный allowlist, а не отключение защиты.

## Корпус и обновление

`data/playbook.lock.json` закрепляет Git commit публичного Avito Playbook. CLI клонирует только
этот репозиторий во временную папку и читает Markdown из Git objects. Ссылки из текстов, картинки,
внешние статьи и другие форматы не скачиваются. Офлайн-вариант — существующий clone с нужным commit:

```sh
python -m homych_knowledge.ingest playbook --repo /path/to/playbook
```

Для обновления поменять commit через review и повторить ingestion. Удалённые файлы исключаются
из нового snapshot. Ошибка embedding, пустой корпус или изменённая существующая ревизия не
переключают active snapshot. `python -m homych_knowledge.ingest revoke <documentId>` немедленно
исключает источник; повторная загрузка не снимает отзыв. Восстановление — решение оператора в БД.

Цитаты ссылаются на конкретный commit. Playbook содержит исторические сведения; загрузка не
подтверждает актуальность корпоративной политики. Технический диапазон дат snapshot 2026–2100
не означает срок действия регламентов. Ответы о назначенном бадди, конкретных доступах и текущем
этапе требуют проверенных данных Go. Оператор CLI управляет публикацией MVP; корпоративная роль
владельца знаний и интерфейс материалов в этой поставке не назначаются.

Поиск: PostgreSQL 16 + pgvector exact cosine, FTS russian/simple top-20, vector top-20, RRF(60),
до двух chunks документа и пяти в результате.
FTS сопоставляет содержательные слова вопроса через OR с весами заголовка/раздела/текста;
simple-канал использует латинские идентификаторы, не русские служебные слова.
Это FTS, не BM25. ACL применяется до ранжирования.
Чанк до 1200 Unicode-символов, overlap до 150 внутри раздела. При отказе embeddings или несовпадении
model digest используется FTS (`lexical_degraded`). Общего кэша ответов нет. Порог 0.50 — стартовая
настройка, не измеренная уверенность. HNSW, reranker и Redis для этого MVP не используются.

Генерация: Qwen3:4b, JSON Schema, `think=false`, до 512 токенов ответа, `num_ctx=8192`.
До подключения точного токенизатора system+user prompt ограничен 6000 UTF-8 bytes; фрагменты
удаляются целиком с конца ранжирования. Полная история чата в модель не передаётся. Валидатор
проверяет структуру и принадлежность цитат; соответствие всех утверждений источникам требует
отдельной ручной оценки. Инструкции из документов не дают полномочий вызывать инструменты.

## Синтетическое демо

`data/fixtures.json` — явно тестовые документы двух команд. Для включения указать
`RAG_SYNTHETIC_SUBJECTS=["u-sasha","u-kirill"]` в `.env`, пересоздать `knowledge` и выполнить:

```sh
docker compose -f docker-compose.yml -f docker-compose.rag.yml exec knowledge \
  python -m homych_knowledge.ingest playbook --fixtures data/fixtures.json
```

Остальные пользователи не видят synthetic-документы. Саша видит только platform, Кирилл — mobile;
признак synthetic возвращается в цитатах. Для запуска без моделей задать `RAG_PROVIDER=deterministic`,
пересоздать сервис и повторить ingestion. Ответы маркируются как тестовые; это не демонстрация
качества живой модели. За пределами `RAG_DEMO_MODE=true` такой провайдер запрещён.

## Передача Go-разработчику

Контракты: [HTTP OpenAPI](contracts/openapi.json), [MCP schema](contracts/mcp-tool.json).
`POST /internal/v1/ai-context` в Go уже реализован, включается при настроенном `RAG_SERVICE_TOKEN`.
Возвращает текущие назначения самого стажёра, для бадди/руководителя/HR — 403. `internshipIds`
содержит ID assignments. Python не читает бизнес-таблицы Go; Go не читает `rag`.

Следующий модуль Go `answers` должен:

1. Принять сессию, CSRF, `Idempotency-Key` и вопрос по публичному контракту. Получить пользователя
   через `identity.Subject`, не из браузерного JSON.
2. Сохранить вопрос и AnswerJob до обращения к Python. Worker должен поддерживать lease/retry и
   сохранение результата один раз по `requestId`. Python вычисляет ответ, durable jobs и историю
   не хранит; повтор вызова может повторно вызвать модель.
3. Вызвать `/internal/v1/answer` с сервисным credential. Необязательный `messageContext` — только
   проверенное сообщение этого стажёра, до 4000 символов, без inbox бадди/руководителя.
4. Сохранить `reply`, `status`, `citations`, `snapshotId`, `retrievalMode`; отдать через `getAnswer`.
   При повторном чтении истории учитывать текущие права источников.
5. Обработать 401/403/404 как отказ доступа, 422 как ошибку контракта, 503 как отказ зависимости.
   Статусы `no_evidence`, `conflicting_sources`, `unavailable` в 200-ответе — ожидаемые исходы,
   не успешный содержательный ответ.

Стартовые configurable budgets: embedding 15 с, generation 45 с, весь запрос 60 с. Это пределы
библиотечной конфигурации, не SLA. Корневой `.env.example` задаёт для локальной CPU-пробы
120/180/240 с соответственно; Compose передаёт эти параметры в Python. Тайм-аут демо-прокси —
250 с (`RAG_DEMO_TIMEOUT`), он должен быть больше общего `RAG_REQUEST_TIMEOUT`.
Go HTTP timeout должен превышать общий budget Python. Календарь
не зависит от доступности AI. Схему `rag` применяет CLI `ingest migrate/playbook`, бизнес-миграции — Go.

Известные ограничения: отсутствие источника в части живых вопросов пока может классифицироваться
как `unavailable` из-за невалидных идентификаторов цитат, а не как `no_evidence`. Полный контекст
периода и долговечная история пока не подключены. [Аудит](../docs/ai-buddy/implementation-audit-2026-11-13.md)
фиксирует эти ограничения и состав оставшихся модулей; сквозная интеграция требует отдельных E2E-тестов.

## Проверки

```sh
cd knowledge
uv sync --frozen
uv run ruff check .
uv run ruff format --check .
uv run pytest
```

Для интеграционных тестов задать `TEST_DATABASE_URL` на PostgreSQL с pgvector и правом CREATE
DATABASE. Для native Windows запускать API через `python -m homych_knowledge.app`: entrypoint
выбирает совместимый с psycopg selector event loop; CLI и тесты используют тот же loop factory.
Каждый тест получает отдельную БД; без переменной они явно skipped. CI проверяет
реальные SQL/ACL, MCP initialize/list_tools/call_tool, Go identity bridge, Docker build и
Compose smoke: реальный Playbook → Go ACL → поиск/ответ → сетевой MCP.
Модель в CI заменена тестовым адаптером.

Retrieval benchmark после ingestion и запуска Go:
`uv run python -m homych_knowledge.evaluate --split test`.
JSON-результат содержит Recall@5, режимы, latency и model digest. В `data/eval.json` разделены
dev/test, стартовая цель test Recall@5 ≥ 0.80. До реального измерения цель не считается достигнутой.
Ручной review генерации нужен отдельно от retrieval benchmark.

Prometheus `/internal/metrics` защищён сервисным credential. Метрики содержат только технические
статусы/длительности. Тексты переписки и tokens не логируются; access log Uvicorn отключён.
Порт Python привязан к loopback. Не публиковать внутренний API в интернет.
