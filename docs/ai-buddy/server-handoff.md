# Передача FRONT серверной команде

> **14.11.2026: передача выполнена.** Все 23 операции реализованы в Go ([backend/README.md](../../backend/README.md)), FRONT работает через них при `VITE_API=on`, AI-ответы идут через Python RAG ([knowledge/README.md](../../knowledge/README.md)). Текущее состояние и отступления — [аудит на 14 ноября](implementation-audit-2026-11-14.md). Ниже — исходный план передачи 12.11.2026, сохранён как основание решений.

12.11.2026 · Проект реализации (исходная редакция).

Основная концепция: [team-specification.md](team-specification.md). Полный перечень поведения: [frontend-functional-specification.md](frontend-functional-specification.md). [OpenAPI](system-analysis/openapi.yaml) — новый проект прикладного API, 23 операции; прежние семь операций сохранены в [openapi-legacy.yaml](system-analysis/openapi-legacy.yaml). Контракт подходит при Qwen или DeepSeek: провайдер вызывается серверным адаптером. MCP и OpenAPI — разные интерфейсы.

## Подключение по шагам

1. Оставить UI и чистые функции domain/scenarios/suggestions, ввести repository/service layer. React-компоненты пока сами обращаются к localStorage; HTTP-клиент ещё не написан. Не пытаться подключить backend простой заменой baseURL.
2. `sessionContext` заменить демопереключатели идентичности. Выбирать assignment только из серверных разрешений; роль браузера не доверенная. Помощник получает собственный inbox, а не личный feed стажёра с клиентским скрытием реплик.
3. `listScenarios/getScenario/createScenario/saveDraft/publishScenario` заменить локальную коллекцию. Нужны draftVersion, ETag/If-Match, сохранение неполных полей, явное предупреждение о конфликте. Backend принимает userId, а не ФИО; адаптер отображает имена из разрешённого directory.
4. `listMessages/setCheck/getSuggestions` заменить runtime. Scheduler работает без открытой вкладки. Данные событий и ролевых снимков создаются транзакционно и независимо от отметок. Пользователь не передаёт server clock; demoDate остаётся только локальным режимом.
5. `askQuestion/getAnswer` заменить setTimeout/demoAnswer. Вопрос и requestId сохраняются до ответа; UI восстанавливает polling. RAG фильтрует ACL до retrieval и до вызова модели; цитаты сохраняют sourceVersionId, но повторное чтение проверяет текущие права.
6. `listSources/getSource/createSource/updateSource/excludeSource/getSourceJob` заменить локальные источники/900 мс. Владелец определяется отдельно; использовать отдельную capability `knowledge:write`, не назначать автоматически HR.
7. Условно после OQ08: `createAccessDraft/confirmAccess/getAccess/getProtectedTask` → небольшой HTTP-мок. Подтверждение человека, ресурс, correlationId, idempotency, номер и статус долговечны. Неизвестный исход восстанавливается сверкой, не второй отправкой.
8. `listNotifications` отдаёт только свои сообщения/доставки. Канал push/мессенджер и read receipt не определены. Звук локального открытого UI не обещает системного уведомления в фоне.

## Обязательные инварианты данных

- Unique active scenario per assignment. Устойчивые ID блока/пункта через draft/revision. Draft не меняет действующую публикацию.
- Unique event `(assignment_id, scenario_id, block_id)` для текущей модели; revision хранится в снимке, но не создаёт повтор ранее отправленного блока того же варианта. Правки будущего блока используют актуальную revision. Не вводить revision в ключ так, чтобы перепубликация дублировала прошлое.
- При замене active-варианта сохранить cutoff даты назначения: новый вариант отправляет только `block.date > cutoffDate`. Первая публикация допускает наступившие даты — как локальный `deliverDue`; окончательно подтвердить политику при OQ04.
- Unique recipient delivery `(event_id, recipient_user_id, role_kind)`; очередь/повторы не меняют снимок. Старый helper не должен получать новый материал после переназначения — политику исторического доступа решить до пилота.
- Check `(message_id, task_id, subject)` не влияет на календарь. Не переносить исторические отметки по названию нового пункта.
- Suggestion selection на assignment: `stageKey = block:<stable blockId>` либо `prestart`; topic сохраняется отдельно. stageKey не содержит revision. При замене сценария с тем же blockId и темой локальный прототип сохраняет подборку; менять это без решения продукта нельзя.
- SourceVersion и AnswerJob неизменяемы в своей истории; чтение всё равно ограничено текущим ACL. Source exclusion убирает из новых ответов, не стирает сохранённый текст ответа автоматически.
- Idempotency record содержит subject, operation, key, canonical payload hash, status и результат. Другой payload с тем же ключом → 409. Срок хранения ключей нужно определить по максимальному окну повторов, не назначать случайно.

## Проект модулей Go

`identity/assignments`, `scenarios/publications`, `scheduler/outbox`, `messages/checks`, `suggestions`, `knowledge/indexing/retrieval`, `answers/modeladapter`, `tools/accessmock`, `audit`. Это модули, не требование развернуть девять сервисов. PostgreSQL отвечает за транзакции и уникальности. Векторный движок и queue transport ещё не выбраны; первоначально допустим worker, читающий задания из PostgreSQL.

RAG и MCP не должны получать полномочия от текста prompt. Сначала сервер проверяет сессию/назначение/ACL, потом вызывает инструмент. Документы могут содержать вредоносные инструкции; их текст не разрешает сетевой вызов или изменение доступа. Модель предлагает черновик, пользователь подтверждает, сервер повторно валидирует ресурс и payload. MCP `tools/call` выполняется только с ограниченным контекстом сервера; annotations — подсказки, не security boundary. Техническая основа: https://modelcontextprotocol.io/specification/latest/server/tools .

## Контракт ошибок и UX

401 — требуется вход; 403 — запрещённое известное действие; 404 — объект не существует или не виден; 409 — конфликт active/idempotency/no active plan; 412 — устаревший ETag; 422 — ошибки полей; 429 — ограничение с Retry-After; 503 — недоступная зависимость. Ответ `Error` содержит безопасный `message`, `code`, `requestId`, допустимые `details`. Введённый текст не очищать при ошибке. Не повторять автоматически неидемпотентную мутацию с новым ключом.

Все адресные объекты проверяются повторно на сервере. При cookie-сессии нужны Origin/CSRF-проверки мутаций, SameSite/HttpOnly/Secure по окружению. SSO handshake не согласован, поэтому контракт не вводит фиктивный password login. Операционные лимиты текста/страниц в OpenAPI — предложения до испытаний. Время ответа модели и retry budget измерить на выбранном CPU.

## Критерии серверной готовности

Два назначения/две команды; отрицательные ACL-тесты feed, source, citation и tool. Публикация/замена в конкурентных транзакциях. Повтор scheduler и повтор HTTP после потери ответа. Падение процесса после commit до transport ack. Восстановление AnswerJob после reload. Неизвестный статус доступа и approved другого ресурса. Модель без источника/с чужим источником/с конфликтом/тайм-аутом. Обновление индекса с сохранением старой версии. Полный запуск без внешнего ключа и отдельный воспроизводимый запуск с реальной выбранной моделью.

Матрица [traceability.csv](traceability.csv) связывает каждую карточку с UI, BPMN, API, данными и заданием AT. AT — критерий, не заявление о реализованном тесте. Демо-переключатели и оформление не требуют отдельной серверной операции.
