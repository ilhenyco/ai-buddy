import os
from datetime import timedelta
from unittest.mock import AsyncMock
from uuid import uuid4

import psycopg
import pytest
import pytest_asyncio
from psycopg import sql

from homych_knowledge.models import AccessContext
from homych_knowledge.provider import DeterministicProvider, ProviderError
from homych_knowledge.store import Store

pytestmark = pytest.mark.integration


@pytest_asyncio.fixture
async def store():
    url = os.getenv("TEST_DATABASE_URL")
    if not url:
        pytest.skip("TEST_DATABASE_URL is required for pgvector integration tests")
    name = "rag_test_" + uuid4().hex
    async with await psycopg.AsyncConnection.connect(url, autocommit=True) as root:
        await root.execute(sql.SQL("CREATE DATABASE {}").format(sql.Identifier(name)))
        from psycopg.conninfo import make_conninfo

        instance = Store(make_conninfo(url, dbname=name))
        try:
            await instance.migrate()
            yield instance
        finally:
            await root.execute(
                sql.SQL("DROP DATABASE {} WITH (FORCE)").format(sql.Identifier(name))
            )


async def test_acl_before_ranking_and_real_hybrid(store, doc, access):
    documents = []
    for n in range(30):
        documents.append(
            doc.model_copy(
                update={
                    "documentId": f"foreign-{n}",
                    "scope": "team",
                    "scopeId": "mobile",
                    "text": "cedar " * 60,
                }
            )
        )
    own = doc.model_copy(update={"documentId": "own", "scope": "team", "scopeId": "platform"})
    documents.append(own)
    provider = DeterministicProvider()
    await store.publish(documents, provider)
    result = await store.search("cedar", access, None, 5, 0.5)
    assert {h.documentId for h in result.items} == {"own"}
    vector = (await provider.embed(["cedar"]))[0]
    hybrid = await store.search("cedar", access, vector, 5, -1, await provider.identity())
    assert hybrid.retrievalMode == "hybrid"
    assert {h.documentId for h in hybrid.items} == {"own"}
    mismatch = await store.search("cedar", access, vector, 5, -1, "different-model")
    assert mismatch.retrievalMode == "lexical_degraded"


async def test_natural_question_retains_lexical_topic_ranking(store, doc, access):
    learning = doc.model_copy(
        update={
            "documentId": "learning",
            "title": "Обучение и развитие",
            "text": "# Тренинги\nКоманда проводит технические тренинги и оплачивает курсы.",
        }
    )
    structure = doc.model_copy(
        update={
            "documentId": "structure",
            "title": "Структура",
            "text": "# Структура\nСотрудники Авито работают в командах. В Авито много сотрудников.",
        }
    )
    await store.publish([learning, structure], DeterministicProvider())
    result = await store.search(
        "Как в Авито устроено обучение и развитие сотрудников?", access, None, 1, 0.5
    )
    assert [item.documentId for item in result.items] == ["learning"]


async def test_snapshot_rollback_immutable_revision_and_revocation(store, doc, access):
    provider = DeterministicProvider()
    snapshot = await store.publish([doc], provider)
    with pytest.raises(ValueError, match="revision"):
        await store.publish([doc.model_copy(update={"text": "Different text"})], provider)
    result = await store.search("cedar", access, None, 5, 0.5)
    assert result.snapshotId == snapshot and result.items
    ids = [h.chunkId for h in result.items]
    assert await store.permitted(access, snapshot, ids)
    await store.revoke(doc.documentId)
    assert not await store.permitted(access, snapshot, ids)
    assert not (await store.search("cedar", access, None, 5, 0.5)).items
    # Re-ingestion must not silently remove an operator's revocation.
    await store.publish([doc], provider)
    assert not (await store.search("cedar", access, None, 5, 0.5)).items


async def test_no_partial_publication_on_embedding_failure(store, doc, access):
    provider = DeterministicProvider()
    snapshot = await store.publish([doc], provider)
    provider.embed = AsyncMock(side_effect=ProviderError())
    with pytest.raises(ProviderError):
        await store.publish(
            [doc.model_copy(update={"revision": "v2", "text": "New text"})], provider
        )
    assert (await store.search("cedar", access, None, 5, 0.5)).snapshotId == snapshot


async def test_synthetic_expiry_internship_and_document_limit(store, doc, access):
    from datetime import UTC, datetime

    now = datetime.now(UTC)
    documents = [
        doc.model_copy(update={"documentId": "synthetic", "synthetic": True}),
        doc.model_copy(
            update={
                "documentId": "expired",
                "validFrom": now - timedelta(days=2),
                "expiresAt": now - timedelta(days=1),
            }
        ),
        doc.model_copy(
            update={
                "documentId": "future",
                "validFrom": now + timedelta(days=1),
                "expiresAt": now + timedelta(days=2),
            }
        ),
        doc.model_copy(
            update={
                "documentId": "personal",
                "scope": "internship",
                "scopeId": "assignment-sasha",
                "text": "cedar " * 1000,
            }
        ),
    ]
    await store.publish(documents, DeterministicProvider())
    result = await store.search("cedar", access, None, 5, 0.5)
    assert len(result.items) == 2
    assert {h.documentId for h in result.items} == {"personal"}
    stranger = AccessContext(
        subjectId="u-kirill", role="intern", teamIds=["mobile"], internshipIds=["assignment-kirill"]
    )
    assert not (await store.search("cedar", stranger, None, 5, 0.5)).items
    access.synthetic = True
    assert "synthetic" in {
        h.documentId for h in (await store.search("cedar", access, None, 5, 0.5)).items
    }
