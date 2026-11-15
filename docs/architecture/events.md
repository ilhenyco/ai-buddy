# Бизнес-события Хомыча (outbox)

Go API записывает бизнес-событие **в той же транзакции**, что и изменение, которое его вызвало, — в таблицу `outbox_events` (пакет `backend/internal/platform/outbox`). Фоновый relay забирает неотправленные события пачками и передаёт их публикатору. Поэтому:

- откат транзакции не оставляет события, а записанное изменение всегда получает своё событие;
- недоступный получатель не теряет события: пачка ждёт и повторяется с растущей паузой (2 с, 4 с … до 5 мин);
- доставка **хотя бы один раз**: получатель отбрасывает дубли по `id` события;
- несколько копий API не отправят одно событие одновременно: строки берутся с `FOR UPDATE SKIP LOCKED`.

**Приватность.** В событиях только идентификаторы, роли и исходы. Тексты вопросов, ответов и материалов в события не попадают никогда: это проверяет тест `TestBusinessEventsOfTheMainPath`.

## Публикаторы

| `EVENTS_PUBLISHER` | Куда | Для чего |
|---|---|---|
| `log` (по умолчанию) | Строка JSON с `"msg":"event"` в лог API | Демо и отладка: `docker compose logs api \| grep '"event"'` |
| `http` | `POST EVENTS_HTTP_URL` с телом `{"events":[…]}`, заголовок `X-Batch-Id`, необязательный `Authorization: Bearer EVENTS_HTTP_TOKEN` | Сборщик Clickstream, webhook или Kafka REST Proxy. Любой ответ 2xx — успех |
| `off` | — | События не записываются и relay не запускается |

Kafka подключается следующим адаптером с тем же интерфейсом `outbox.Publisher` (`Publish(ctx, []Envelope) error`): топик `homych.events`, ключ сообщения — `aggregate.id`. До этого её можно получить через `http` и Kafka REST Proxy.

## Конверт

```json
{
  "id": "oev-f907816bcef1356f",
  "type": "answer.completed",
  "version": 1,
  "occurredAt": "2026-11-15T17:17:25.701204Z",
  "aggregate": {"type": "answer", "id": "ans-e21fe117a1c433a4"},
  "assignmentId": "assignment-sasha",
  "teamId": "platform",
  "actor": null,
  "data": {"answerId": "ans-e21fe117a1c433a4", "outcome": "answered", "retrievalMode": "hybrid", "citations": 1, "errorCode": null, "attempt": 1}
}
```

`actor` — пользователь, вызвавший событие (`{"userId","role"}`), или `null` для планировщика и фоновых обработчиков. `occurredAt` — реальное время; дата этапа по календарю плана — в `data.blockDate`. При новых полях в `data` версия не меняется; при несовместимом изменении растёт `version`.

## Каталог

| Тип | Когда | Агрегат | Актор | `data` |
|---|---|---|---|---|
| `scenario.published` | Руководитель опубликовал сценарий | `scenario` | руководитель | `scenarioId`, `revisionId`, `revisionNumber`, `blocks`, `cutoffDate`, `replacedScenarioId` |
| `stage.delivered` | Планировщик разослал этап (одно событие на блок) | `stage` | — | `scenarioId`, `revisionId`, `blockId`, `blockDate`, `tasks`, `recipients` |
| `checklist.item_checked` | Участник отметил пункт (повторная отметка — не событие) | `message` | отметивший | `messageId`, `taskId`, `blockId` |
| `checklist.item_unchecked` | Участник снял отметку | `message` | снявший | `messageId`, `taskId`, `blockId` |
| `question.asked` | Стажёр задал вопрос Хомычу | `answer` | стажёр | `answerId` |
| `answer.completed` | Ответ сохранён | `answer` | — | `answerId`, `outcome` (`answered`, `no_evidence`, `conflicting_sources`, …), `retrievalMode`, `citations`, `attempt` |
| `answer.failed` | Ответа не будет (модель недоступна или отклонила запрос) | `answer` | — | `answerId`, `errorCode` (`AI_UNAVAILABLE`, `AI_REJECTED`, `AI_DISABLED`), `attempt` |
| `source.version_created` | Руководитель добавил материал или новую версию | `source` | руководитель | `sourceId`, `versionId`, `category`, `jobId` |
| `source.excluded` | Руководитель исключил материал | `source` | руководитель | `sourceId`, `versionId`, `category`, `jobId` |
| `source.indexed` | Версия попала в поиск | `source` | — | `sourceId`, `versionId`, `jobId` |
| `source.removed` | Материал убран из поиска | `source` | — | `sourceId`, `versionId`, `jobId` |
| `source.index_failed` | Индексация не удалась | `source` | — | `sourceId`, `versionId`, `jobId`, `errorCode` |
| `access.requested` | Стажёр подтвердил заявку на доступ | `access_operation` | стажёр | `operationId`, `resourceId`, `correlationId` |
| `access.decided` | Сервис доступов принял решение (записывается один раз) | `access_operation` | — | `operationId`, `status` (`approved`, `denied`) |

## Метрики пользы из событий

| Метрика | Как считать |
|---|---|
| Доля пройденных этапов | `checklist.item_checked` минус `unchecked` по `blockId` к числу пунктов из `stage.delivered.tasks` |
| Время до получения доступа | `access.decided.occurredAt − access.requested.occurredAt` по `operationId` |
| Вопросы без ответа — пробелы в базе знаний | доля `answer.completed` с `outcome = no_evidence` и `answer.failed` среди `question.asked`, по команде |
| Свежесть базы знаний | `source.indexed` по командам и времени |
| Активность руководителей | `scenario.published` по командам |

## Наблюдаемость

Prometheus (`GET /metrics`): `homych_outbox_published_total{type}`, `homych_outbox_publish_failures_total`, `homych_outbox_pending` — сколько событий ждёт отправки. Растущий `pending` означает, что получатель недоступен.

## Хранение

Отправленные события остаются в `outbox_events` (`published_at`). Срок хранения и очистку определяет эксплуатация; сброс демо-стенда таблицу не очищает.
