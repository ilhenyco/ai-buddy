from unittest.mock import AsyncMock

from homych_knowledge.bootstrap import bootstrap


async def test_existing_index_is_kept():
    store = AsyncMock()
    store.active_digest.return_value = "digest"
    ingest = AsyncMock()
    assert await bootstrap(store, ingest, log=lambda *_: None)
    store.migrate.assert_awaited_once()
    ingest.assert_not_awaited()


async def test_empty_index_is_ingested_with_retries():
    store = AsyncMock()
    store.active_digest.return_value = None
    ingest = AsyncMock(side_effect=[RuntimeError("model not pulled yet"), None])
    sleep = AsyncMock()
    assert await bootstrap(store, ingest, sleep=sleep, log=lambda *_: None)
    assert ingest.await_count == 2
    sleep.assert_awaited_once()


async def test_gives_up_after_attempts():
    store = AsyncMock()
    store.active_digest.return_value = None
    ingest = AsyncMock(side_effect=RuntimeError("ollama is down"))
    assert not await bootstrap(store, ingest, attempts=3, sleep=AsyncMock(), log=lambda *_: None)
    assert ingest.await_count == 3
