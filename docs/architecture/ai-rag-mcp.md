# Техническая документация AI-части: RAG и MCP

Дата: 14 ноября 2026 года. Документ описывает реализацию в ветке `codex/rag-mcp`.
Аудитория: разработчик внешнего агента, интегратор корпоративных данных, оператор
сервера и разработчик Go-бэкенда. Возможности, обозначенные как последующие доработки,
не входят в текущий контракт.

## 1. Назначение и способы использования

Сервис предоставляет поиск по базе знаний с проверкой доступа и ссылками на версии
источников. Предусмотрены два варианта эксплуатации:

| Вариант | Кто подключается | Что требуется |
|---|---|---|
| Отдельный сервер знаний | Внешний агент или приложение Авито | HTTPS-адрес, клиентский ключ, MCP-клиент либо HTTP-клиент |
| AI-часть приложения «Хомыч» | Наш Go-бэкенд, локальный оператор демо | Go, PostgreSQL, Python-сервис, Ollama, внутренний сервисный ключ |

Для первого варианта Авито не требуется переносить наш проект в свою кодовую базу,
разворачивать наш фронтенд или заводить стажёров в нашем Go-приложении. Сервер знаний
разворачивает оператор; потребитель настраивает удалённое соединение.

Нужно настроить два независимых потока:

1. **Использование знаний:** агент → MCP/HTTP → поиск → доступные источники.
2. **Обновление знаний:** корпоративная система → API документов → очередь → индекс.

MCP возвращает материалы для ответа. Ответ пользователю формирует модель внешнего
агента. Генерация нашим Qwen доступна через внутренний API, а не внешний MCP.

## 2. Компоненты и границы ответственности

| Компонент | Назначение |
|---|---|
| Python FastAPI / FastMCP | Внешние MCP/HTTP-интерфейсы, авторизация клиентов, внутренний API |
| KnowledgeService | Единая логика поиска и проверка доступа; внутренняя генерация ответа |
| PostgreSQL 16 + pgvector | Документы, векторы, полнотекстовый индекс, снимки и задания загрузки |
| Ollama / BGE-M3 | Эмбеддинги документов и вопросов, размерность 1024 |
| Ollama / Qwen3:4b | Генерация через внутренний `/internal/v1/answer` |
| Python worker | Фоновая обработка изменений корпоративного корпуса |
| Caddy | HTTPS и публикация только внешних маршрутов |
| Go | Идентичность и назначения стажёров при использовании нашего приложения |

Для внешних сервисных клиентов Go не вызывается: разрешённые команды задаются
оператором в конфигурации клиента. Для демо-пользователей Python получает актуальные
назначения через `POST /internal/v1/ai-context` в Go.

Один отдельный экземпляр сервиса предназначен для одной организации. Изоляция
нескольких организаций внутри одной БД не реализована. Для другой организации
нужен отдельный экземпляр с отдельным хранилищем и ключами.

## 3. Развёртывание отдельного сервера

### 3.1. Предварительные условия

- Docker Engine и Docker Compose с поддержкой используемого Compose-файла.
- Домен с DNS-записью на сервер; входящие TCP 80 и 443 для Caddy и получения TLS-сертификата.
- Доступ к реестрам контейнеров и Ollama для первоначальной загрузки образов и модели.
- Постоянные дисковые тома, резервное копирование PostgreSQL и достаточные ресурсы
  для выбранного корпуса. Проверенный минимальный объём RAM и нагрузочный SLA не установлены.

Все команды развёртывания ниже выполняются из корня репозитория. Репозиторий нужен
оператору сервера; внешнему потребителю достаточно адреса и ключа.

### 3.2. Подготовка настроек

В PowerShell:

```powershell
git switch codex/rag-mcp
Copy-Item knowledge/external.env.example .env.external
python knowledge/examples/keygen.py
python knowledge/examples/keygen.py
```

Первый ключ предназначьте читателю, второй — загрузчику документов. Скрипт выводит
исходный токен и SHA-256. Исходный токен передаётся клиенту, сервер хранит только хеш.
Не используйте один токен для обоих клиентов или для внутреннего API.

Заполните `.env.external`:

| Переменная | Значение |
|---|---|
| `KNOWLEDGE_DOMAIN` | Домен без схемы и пути, например `knowledge.company.ru` |
| `KNOWLEDGE_DB_PASSWORD` | Случайный пароль БД из URL-safe символов |
| `RAG_SERVICE_TOKEN` | Отдельный случайный внутренний ключ, минимум 24 символа |
| `RAG_EXTERNAL_CLIENTS` | JSON-объект зарегистрированных клиентов |

Структура `RAG_EXTERNAL_CLIENTS` показана ниже. `READER_SHA256` и `WRITER_SHA256`
не являются допустимыми хешами: замените их выводом скрипта. В dotenv JSON должен
находиться на одной строке, как в `knowledge/external.env.example`.

```json
{
  "avito-reader": {
    "token_sha256": "READER_SHA256",
    "scopes": ["knowledge:search", "knowledge:read"],
    "team_ids": ["platform"],
    "expires_at": "2027-01-01T00:00:00Z"
  },
  "avito-writer": {
    "token_sha256": "WRITER_SHA256",
    "scopes": ["documents:write"],
    "team_ids": [],
    "expires_at": "2027-01-01T00:00:00Z"
  }
}
```

Укажите будущую дату истечения с часовым поясом. Ключи генерируются из случайных
данных; пароль человека вместо токена использовать не следует. `.env.external`
исключён из Git общими правилами `.gitignore`.

### 3.3. Запуск

```powershell
docker compose --env-file .env.external -f docker-compose.knowledge.yml config --quiet
docker compose --env-file .env.external -f docker-compose.knowledge.yml up -d --build
docker compose --env-file .env.external -f docker-compose.knowledge.yml exec ollama ollama pull bge-m3:567m
docker compose --env-file .env.external -f docker-compose.knowledge.yml ps
```

Worker создаёт схему БД автоматически. Перед отправкой первых документов дождитесь
загрузки BGE-M3. Qwen для внешнего MCP-поиска устанавливать не требуется.

Снаружи опубликованы только маршруты `/mcp`, `/mcp/*`, `/v1/*` через HTTPS.
PostgreSQL, Ollama, внутренний API, метрики и health endpoints наружу не опубликованы.
Получение сертификата и доступ через домен проверяются при реальном развёртывании;
локальная проверка контейнеров не заменяет такую проверку.

### 3.4. Первоначальный корпус

Можно сразу загружать корпоративные документы через API из раздела 6. Для первичного
демо также доступен импорт публичного Playbook:

```powershell
docker compose --env-file .env.external -f docker-compose.knowledge.yml exec knowledge python -m homych_knowledge.ingest playbook
```

Ревизия Playbook закреплена в `knowledge/data/playbook.lock.json`:
`0c1d2aa1501ca6889357908f54b12695f0534e48`. Загрузчик читает Markdown из этого Git commit;
внешние ссылки, картинки и вложенные сайты не скачиваются. Playbook не подтверждает
актуальность внутренних политик Авито и не содержит автоматически подключённых
персональных HR-данных.

**Команда `ingest playbook` заменяет весь корпус.** Используйте её для начального
наполнения до потока корпоративных изменений. Для дальнейших обновлений используйте
API документов. В старой БД, созданной до появления очереди, требуется восстановить
исходные JSON документов повторным импортом либо контролируемой загрузкой полного
корпуса; одни старые chunks не заменяют исходные документы. До этого не запускайте
поток изменений worker, чтобы он не опубликовал неполный корпус.

## 4. Авторизация и права

Все внешние запросы передают `Authorization: Bearer CLIENT_TOKEN`. Используется
частный профиль с заранее выданными ключами; OAuth discovery, SSO и вход от имени
сотрудника не реализованы. OAuth-only клиент без поддержки настроенного Bearer-ключа
пока не сможет подключиться по этому профилю.

| Scope | Возможность |
|---|---|
| `knowledge:search` | Подключение к MCP, поиск через MCP и `POST /v1/search` |
| `knowledge:read` | Дополнительно разрешает MCP-чтение источника; для MCP-соединения также нужен `knowledge:search` |
| `documents:write` | Администрирование всего корпуса: загрузка, удаление, просмотр и повтор заданий своего клиента |

Клиент с одним `documents:write` не получает доступ к поиску. Не выдавайте это право
обычному агенту: загрузчик может менять не только текст, но и ACL документов.

| Область документа | Кто видит |
|---|---|
| `common` | Все клиенты с правом поиска в этом экземпляре организации |
| `team` | Клиенты, у которых `team_ids` содержит `scopeId` документа |
| `internship` | Назначенный стажёр через Go-контекст; внешнему сервисному клиенту недоступно |
| `synthetic=true` | Только специально разрешённые пользователи демо; внешним сервисным клиентам недоступно |

Клиент не передаёт `userId`, `subjectId`, роль или команды в MCP-инструменте.
Scope клиента и ACL документов проверяются сервером. При отсутствии подходящих
материалов возвращается пустая выдача без раскрытия закрытых документов.

Для отзыва ключа удалите клиента из `RAG_EXTERNAL_CLIENTS` и пересоздайте `knowledge`
и `worker` командой `up -d --force-recreate knowledge worker` с тем же Compose-файлом
и env-файлом. При ротации добавьте нового клиента, переключите потребителя, удалите
старого. Доступ к старым jobs привязан к прежнему client ID, поэтому перед удалением
загрузчика завершите или обслужите его незавершённые задания. Срок ключа проверяется
на каждом запросе; изменения конфигурации требуют пересоздания процессов.

## 5. Использование MCP и HTTP-поиска

### 5.1. Параметры соединения

| Параметр | Значение |
|---|---|
| Адрес | `https://YOUR_DOMAIN/mcp/` — с завершающим `/` |
| Транспорт | Streamable HTTP, stateless, JSON responses |
| Авторизация | Заголовок `Authorization: Bearer CLIENT_TOKEN` |
| Инструменты | `search_knowledge`, `get_knowledge_document` |
| Версия протокола | Установленный SDK поддерживает версии до `2025-11-25`; новые версии требуют проверки |

Названия полей URL и headers в настройках зависят от клиента. Сервер не предоставляет
stdio entrypoint или отдельный legacy SSE endpoint. Проверенный пример на Python
использует настоящий MCP SDK и выполняет initialize → list_tools → call_tool.

В PowerShell, из каталога `knowledge`, с установленным `uv`:

```powershell
uv sync --frozen
$env:MCP_URL = 'https://YOUR_DOMAIN/mcp/'
$env:MCP_TOKEN = 'CLIENT_TOKEN'
uv run python examples/client.py
```

Значение токена замените своим ключом; не включайте ключи в репозиторий или URL.
Для браузерного клиента потребуется отдельная настройка CORS. Текущая конфигурация
рассчитана на серверных клиентов; Host/Origin allowlist не заменяет CORS.

### 5.2. Инструменты

| Инструмент | Аргументы | Результат |
|---|---|---|
| `search_knowledge` | `query`: непустая строка до 1000 символов; `limit`: 1–5, по умолчанию 5 | До limit фрагментов, не более двух от документа |
| `get_knowledge_document` | `documentId`: 1–48 символов; `revision`: 1–80 символов, из результата поиска | До 20 доступных фрагментов указанной активной версии |

Примеры arguments для `tools/call`:

```json
{"query": "Как устроено обучение и развитие?", "limit": 3}
```

```json
{"documentId": "corporate-onboarding", "revision": "v1"}
```

Чтение источника не гарантирует возврат всего большого документа: ограничение —
20 фрагментов, пагинации пока нет. Старая версия после обновления не доступна через
этот инструмент, хотя её исторический снимок может оставаться в БД.

Результат находится в `structuredContent` MCP. Пример структуры; значения score,
идентификаторов и snapshotId ниже условные:

```json
{
  "snapshotId": "snap-example",
  "retrievalMode": "hybrid",
  "items": [{
    "chunkId": "corporate-onboarding-example",
    "documentId": "corporate-onboarding",
    "revision": "v1",
    "title": "Корпоративное обучение",
    "section": "Обучение",
    "url": "https://example.org/onboarding",
    "synthetic": false,
    "text": "Расписание вводных курсов публикуется на учебном портале.",
    "score": 0.02
  }]
}
```

`hybrid` означает сочетание векторного и полнотекстового поиска;
`lexical_degraded` — только полнотекстовый поиск. `score` — показатель ранжирования,
не вероятность истинности ответа. Пустой `items` не является ошибкой транспорта.

Агент должен рассматривать текст источников как недоверенные данные, использовать
его для ответа и сохранять ссылки на источники. Инструкции внутри документа не дают
агенту прав выполнять действия. MCP-инструменты только читают знания: они не создают
заявки, не выдают доступы и не меняют бизнес-данные.

### 5.3. HTTP для приложений без MCP

Этот интерфейс использует тот же поиск и ACL. Пример PowerShell:

```powershell
$baseUrl = 'https://YOUR_DOMAIN'
$readerHeaders = @{ Authorization = "Bearer $env:MCP_TOKEN" }
$query = @{ query = 'Как устроено обучение?'; limit = 5 } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri "$baseUrl/v1/search" -Headers $readerHeaders -ContentType 'application/json; charset=utf-8' -Body ([Text.Encoding]::UTF8.GetBytes($query))
```

Ответ — JSON `SearchResult`, без внешней оболочки MCP `structuredContent`.
Публичного HTTP endpoint генерации ответа или чтения документа сейчас нет.

## 6. Загрузка и обновление корпоративной базы

### 6.1. Контракт документа

Полный пример находится в `knowledge/examples/document.json`.

| Поле | Требование |
|---|---|
| `documentId` | Стабильный ID, 1–48 символов: латиница, цифры, `_`, `-`; совпадает с ID в URL запроса |
| `revision` | Непустая версия до 80 символов; изменяется при изменении текста, ACL или метаданных |
| `title`, `text` | Строки; текст не должен быть пустым; предпочтителен структурированный Markdown |
| `url` | HTTPS-ссылка на оригинал, без логина/пароля в URL |
| `scope` | `common`, `team` или `internship`; по умолчанию `common` |
| `scopeId` | null для common; непустой ID команды/назначения для остальных областей |
| `synthetic` | Для внешней загрузки только false; по умолчанию false |
| `validFrom`, `expiresAt` | Даты с часовым поясом; expiresAt позже validFrom |

Дополнительные поля запрещены. Максимум тела запроса загрузки в Python — 1 000 000
байт; gateway также ограничивает размер. Документы вне срока действия не выдаются.
API принимает готовый текстовый JSON, не файлы PDF/DOCX или URL для скачивания.
Извлечение текста из корпоративных файлов выполняется до отправки документа.

### 6.2. Отправка, контроль результата и удаление

Все следующие примеры PowerShell выполняются из корня репозитория:

```powershell
$baseUrl = 'https://YOUR_DOMAIN'
$env:WRITER_TOKEN = 'WRITER_CLIENT_TOKEN'
$writerHeaders = @{
  Authorization = "Bearer $env:WRITER_TOKEN"
  'Idempotency-Key' = 'onboarding-v1'
}
$document = Get-Content knowledge/examples/document.json -Raw -Encoding UTF8
$receipt = Invoke-RestMethod -Method Put -Uri "$baseUrl/v1/documents/corporate-onboarding" -Headers $writerHeaders -ContentType 'application/json; charset=utf-8' -Body ([Text.Encoding]::UTF8.GetBytes($document))
$jobId = $receipt.jobId
Invoke-RestMethod -Uri "$baseUrl/v1/jobs/$jobId" -Headers $writerHeaders
```

PUT возвращает HTTP 202 с `{ "jobId": "job-...", "status": "pending" }`.
Это подтверждение приёма, а не успешной индексации. Опрос GET возвращает поля
`id`, `status`, `attempts`, `error_code`, `created_at`, `finished_at`.
Задание доступно только создавшему его клиенту.

Для обновления отправьте тот же documentId, новую revision и новый Idempotency-Key:

```powershell
$updated = $document | ConvertFrom-Json
$updated.revision = 'v2'
$updated.text = "# Обучение`nАктуальное расписание доступно на учебном портале."
$writerHeaders['Idempotency-Key'] = 'onboarding-v2'
$body = $updated | ConvertTo-Json -Depth 10
Invoke-RestMethod -Method Put -Uri "$baseUrl/v1/documents/corporate-onboarding" -Headers $writerHeaders -ContentType 'application/json; charset=utf-8' -Body ([Text.Encoding]::UTF8.GetBytes($body))
```

Удаление:

```powershell
$writerHeaders['Idempotency-Key'] = 'onboarding-delete-v2'
Invoke-RestMethod -Method Delete -Uri "$baseUrl/v1/documents/corporate-onboarding" -Headers $writerHeaders
```

Idempotency-Key обязателен для PUT/DELETE, длина 1–80 символов. Повтор одинакового
запроса с тем же ключом возвращает прежний jobId. Другой payload с тем же ключом —
409. Ключи изолированы по client ID. Та же revision с другим текстом, ACL или
метаданными также приводит к 409: ревизия неизменяема.

### 6.3. Очередь и восстановление

Статусы задания: `pending` → `processing` → `completed` либо `failed`.
Worker проверяет очередь раз в пять секунд, сериализует публикации и делает до трёх
попыток обработки. Незавершённые задания сохраняются при перезапуске процесса.

После постановки изменения или удаления старая версия документа сразу скрывается
из поиска. Успешная обработка атомарно публикует новый снимок и завершает задание.
При сбое остальные документы остаются доступными; изменяемый документ остаётся
скрытым. Удаление исключает документ из активного корпуса, но не стирает автоматически
исторические снимки и записи jobs: это не операция полного уничтожения данных.

После трёх неудач очередь останавливается на задании, чтобы поздняя версия не
обогнала необработанную раннюю. Исправьте причину, например загрузите отсутствующую
модель, затем повторите job с ключом его владельца:

```powershell
Invoke-RestMethod -Method Post -Uri "$baseUrl/v1/jobs/$jobId/retry" -Headers $writerHeaders
```

Retry возвращает 202 и сбрасывает failed-задание в pending. Другой статус или
недоступный этому клиенту job — 409. Перезапуск worker сам по себе не сбрасывает
исчерпанный счётчик попыток. В job возвращается технический `INGESTION_FAILED`,
без текста документа или подробностей провайдера.

### 6.4. Подключение постоянного источника и рост корпуса

Готовый универсальный интерфейс синхронизации — push API документов. Коннектор на
стороне источника должен хранить стабильные documentId, отслеживать версии и отправлять
PUT при изменении, DELETE при удалении и новый ACL при изменении доступа.
Изменение ACL требует новой revision. Периодический pull из корпоративных порталов,
webhooks и автоматическое обновление Git ещё не реализованы.

При индексации переиспользуются векторы совпадающих заголовка, раздела и текста,
созданные тем же digest embedding-модели. Это уменьшает повторные вычисления.
Однако текущая публикация создаёт полный новый снимок и копирует в него векторы.
Исторические snapshots, jobs и ключи идемпотентности автоматически не очищаются.

Для большого корпуса нужно отдельно определить политику хранения, пакетную обработку
изменений, резервное копирование и измерить скорость поиска. Сейчас используются
exact cosine и PostgreSQL FTS, без HNSW/ANN, reranker и распределённых workers.
Количество векторов может расти, но гарантии неизменной задержки при росте нет.
Смена embedding-модели требует переиндексации корпуса; несовпадающие digest не
используются для совместного векторного поиска.

## 7. Локальное RAG-демо и интеграция с Go

Для стенда с нашим приложением из корня репозитория:

```powershell
Copy-Item .env.example .env
# Замените RAG_SERVICE_TOKEN в .env случайным ключом минимум 24 символа.
docker compose -f docker-compose.yml -f docker-compose.rag.yml up -d --build
docker compose -f docker-compose.yml -f docker-compose.rag.yml exec ollama ollama pull bge-m3:567m
docker compose -f docker-compose.yml -f docker-compose.rag.yml exec ollama ollama pull qwen3:4b
docker compose -f docker-compose.yml -f docker-compose.rag.yml exec knowledge python -m homych_knowledge.ingest playbook
docker compose -f docker-compose.yml -f docker-compose.rag.yml --profile rag-demo up -d --build --wait
```

Не перезаписывайте существующий `.env`: Copy-Item нужен только при первом запуске.
Отдельная страница вопросов: `http://127.0.0.1:8092/`. Python API:
`http://127.0.0.1:8090`. Эти порты могут быть изменены через HOMYCH_DEMO_PORT и
HOMYCH_RAG_PORT; при занятом порте задайте override до запуска.

Страница фиксирует демо-стажёра на сервере и не проверяет пользовательскую сессию.
Это локальный диагностический интерфейс, не основной AI-чат фронтенда.
В `docker-compose.rag.yml` включён demo-режим. Для теста без живой модели можно
использовать `RAG_PROVIDER=deterministic` и повторно опубликовать корпус; это проверка
интеграции, а не качества генерации.

Для демо-MCP в `.env` задаётся отдельное сопоставление:

```dotenv
RAG_MCP_TOKENS={"SEPARATE_RANDOM_TOKEN_AT_LEAST_24_CHARACTERS":"u-sasha"}
```

После изменения пересоздайте knowledge. Такой ключ работает только в demo-режиме
и связывается с назначенным стажёром через Go. Не путайте его с production-профилем
RAG_EXTERNAL_CLIENTS из раздела 4.

Внутренние Python endpoints используют только RAG_SERVICE_TOKEN и доверенный
серверный `subjectId`:

| Endpoint | Вход / назначение |
|---|---|
| `POST /internal/v1/search` | subjectId, query, limit; поиск с Go-проверкой назначений |
| `POST /internal/v1/answer` | subjectId, query, requestId, limit; опциональный messageContext |
| `GET /internal/metrics` | Prometheus, защищён внутренним ключом |

Пример генерации из PowerShell; RAG_SERVICE_TOKEN должен быть загружен в окружение
оператора отдельно — Compose env-файл не экспортирует его в текущий терминал:

```powershell
$internalHeaders = @{ Authorization = "Bearer $env:RAG_SERVICE_TOKEN" }
$body = @{ subjectId = 'u-sasha'; requestId = 'demo-1'; query = 'Как устроено обучение и развитие?' } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:8090/internal/v1/answer' -Headers $internalHeaders -ContentType 'application/json; charset=utf-8' -Body ([Text.Encoding]::UTF8.GetBytes($body))
```

AnswerResult содержит requestId, status, reply, citations, snapshotId, retrievalMode.
Возможные status: `answered`, `no_evidence`, `conflicting_sources`, `unavailable`.
Клиент должен проверять status, даже если HTTP-запрос завершился успешно.

Go должен определять пользователя по сессии, проверять владение перепиской и сохранять
историю/AnswerJob до обращения к Python. Эти публичные операции и подключение AI-чата
к Python пока не реализованы. Сервисный ключ нельзя передавать браузеру. Внутренние
маршруты нельзя публиковать через внешний gateway.

## 8. Диагностика, ошибки и обслуживание

| Ситуация | Действие |
|---|---|
| HTTP 401 | Проверить ключ, заголовок, срок действия и применение обновлённой конфигурации |
| HTTP 403 / INSUFFICIENT_SCOPE | Проверить scopes; writer без knowledge:search не может искать |
| Пустой items | Проверить публикацию, ACL, сроки документа и незавершённые задания; закрытые документы не раскрываются |
| HTTP 404 для job | Проверить jobId и client ID владельца |
| HTTP 409 | Проверить revision, повторное использование Idempotency-Key или статус retry-задания |
| HTTP 413 | Уменьшить нормализованный документ до лимита тела запроса |
| HTTP 422 | Проверить схему, даты, scopeId и непустые строки |
| HTTP 503 / DEPENDENCY_UNAVAILABLE | Проверить БД и зависимости; повторить с ограниченным backoff |
| MCP isError=true | Проверить ошибку инструмента; отсутствие права read не устраняется повтором того же вызова |
| lexical_degraded | Проверить Ollama, BGE-M3 и соответствие digest активного индекса |
| Очередь не движется | Проверить worker и первое незавершённое задание, особенно attempts ≥ 3 |

Ошибки HTTP API сейчас имеют разные формы (`code` у middleware либо `detail` у
валидации/HTTPException). Не считайте произвольный текст сообщения стабильным кодом.
MCP-ошибка инструмента передаётся через протокол и может не сопровождаться HTTP 4xx.

Проверки изнутри отдельного сервиса:

```powershell
docker compose --env-file .env.external -f docker-compose.knowledge.yml logs --tail 100 worker
docker compose --env-file .env.external -f docker-compose.knowledge.yml exec knowledge python -c "import urllib.request; print(urllib.request.urlopen('http://localhost:8090/healthz').read().decode())"
docker compose --env-file .env.external -f docker-compose.knowledge.yml exec knowledge python -c "import urllib.request; print(urllib.request.urlopen('http://localhost:8090/readyz').read().decode())"
```

healthz проверяет процесс, readyz — доступность активного снимка, но не готовность
всех моделей и внешних зависимостей. До первого опубликованного корпуса readyz
возвращает 503. Таймауты запросов и моделей задаются RAG_REQUEST_TIMEOUT,
RAG_EMBEDDING_TIMEOUT и RAG_GENERATION_TIMEOUT в настройках Python; для изменения
в Compose их нужно явно добавить в environment соответствующего файла.

Перед обновлением сохраняйте backup БД и конфигурацию. Схема обновляется worker
или командой `python -m homych_knowledge.ingest migrate` внутри knowledge. Политики
RPO/RTO, очистки истории и полного уничтожения удалённых данных пока не установлены.
Команда Compose down сохраняет named volumes; down -v удаляет данные и не должна
использоваться для обычной остановки рабочего сервиса.

## 9. Приёмка интеграции и текущие ограничения

Для проверки нового развёртывания:

1. Подключить внешний MCP-клиент по HTTPS и выполнить initialize/list_tools.
2. Загрузить документ writer-ключом и дождаться completed.
3. Найти документ reader-ключом, прочитать его точную версию, проверить ссылку.
4. Обновить документ с новой revision и убедиться, что старая версия не выдаётся.
5. Проверить, что reader не может загружать документы, а чужая команда не видит team-источник.
6. Удалить документ и проверить его отсутствие в выдаче сразу после приёма удаления.
7. Повторить запрос с тем же ключом идемпотентности и проверить прежний jobId.
8. Проверить истёкший/отозванный ключ, восстановление worker и поведение при недоступности модели.

В репозитории есть автоматические тесты MCP, ACL, клиентских прав, expiry,
идемпотентности, jobs и работы с PostgreSQL. Запуск из knowledge:

```powershell
uv sync --frozen
uv run ruff check .
uv run ruff format --check .
uv run pytest
```

Для интеграционных тестов задайте TEST_DATABASE_URL на PostgreSQL с pgvector и
правом CREATE DATABASE. Без переменной эти тесты пропускаются. Нельзя указывать
рабочую производственную БД вместо изолированного тестового стенда.

В текущей поставке отсутствуют OAuth/SSO, мультитенантность, готовые корпоративные
коннекторы, импорт бинарных документов, клиентские квоты и автоматическая очистка
истории. Нет подтверждённого SLA для большого корпуса и проверки всех desktop-клиентов.
Генерация требует отдельной оценки качества: структурная проверка цитат не гарантирует,
что каждое утверждение модели подтверждено источником.

Машиночитаемые контракты: [MCP tools](../../knowledge/contracts/mcp-tool.json) и
[HTTP OpenAPI](../../knowledge/contracts/openapi.json). Примеры: [Python-клиент](../../knowledge/examples/client.py),
[генератор ключей](../../knowledge/examples/keygen.py), [документ](../../knowledge/examples/document.json).
Дополнительные инструкции: [внешний сервер](../../knowledge/EXTERNAL.md),
[локальный RAG и передача Go-разработчику](../../knowledge/README.md).
