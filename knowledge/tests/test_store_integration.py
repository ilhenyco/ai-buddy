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


async def test_incremental_jobs_retries_delete_and_reembedding(store, doc, access):
    from fastapi import HTTPException

    from homych_knowledge.external import enqueue
    from homych_knowledge.worker import process_one

    provider = DeterministicProvider()
    await store.publish([doc], provider)
    uploaded = doc.model_copy(update={"documentId": "uploaded"})
    receipt = await enqueue(store, "writer", "first", "uploaded", uploaded.model_dump(mode="json"))
    assert (
        await enqueue(store, "writer", "first", "uploaded", uploaded.model_dump(mode="json"))
        == receipt
    )
    with pytest.raises(HTTPException) as error:
        await enqueue(store, "writer", "first", "uploaded", None)
    assert error.value.status_code == 409
    await process_one(store, provider)
    assert (await store.document(access, "uploaded", "v1")).items
    # Exact content under a new revision reuses stored embeddings.
    provider.embed = AsyncMock(side_effect=AssertionError("unchanged text must reuse vectors"))
    updated = uploaded.model_copy(update={"revision": "v2"})
    await enqueue(store, "writer", "second", "uploaded", updated.model_dump(mode="json"))
    assert not (await store.document(access, "uploaded", "v1")).items
    await process_one(store, provider)
    assert (await store.document(access, "uploaded", "v2")).items
    assert (await store.document(access, doc.documentId, "v1")).items

    await enqueue(store, "writer", "delete", "uploaded", None)
    assert not (await store.document(access, "uploaded", "v2")).items
    await process_one(store, provider)
    assert not (await store.document(access, "uploaded", "v2")).items
    assert (await store.document(access, doc.documentId, "v1")).items


async def test_external_upload_retry_receipt_and_ownership(store, doc):
    import hashlib

    import httpx

    from homych_knowledge.app import create_app
    from homych_knowledge.config import Settings
    from homych_knowledge.service import GoIdentity, KnowledgeService
    from homych_knowledge.worker import process_one

    token = "writer-credential-with-at-least-32-random-chars"
    settings = Settings(
        database_url=store.database_url,
        service_token="internal-separate-credential-32-chars",
        external_clients={
            "writer": {
                "token_sha256": hashlib.sha256(token.encode()).hexdigest(),
                "scopes": ["documents:write", "knowledge:search"],
                "expires_at": "2030-01-01T00:00:00Z",
            }
        },
    )
    provider = DeterministicProvider()
    service = KnowledgeService(settings, store, provider, GoIdentity(settings, None))
    app = create_app(settings, service)
    async with httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app),
        base_url="http://localhost",
        headers={"Authorization": "Bearer " + token, "Idempotency-Key": "first"},
    ) as client:
        uploaded = await client.put(
            "/v1/documents/" + doc.documentId, json=doc.model_dump(mode="json")
        )
        assert uploaded.status_code == 202
        job_id = uploaded.json()["jobId"]
        broken = AsyncMock()
        broken.identity.side_effect = ProviderError("offline")
        for _ in range(3):
            assert await process_one(store, broken)
        assert not await process_one(store, provider)
        status = (await client.get("/v1/jobs/" + job_id)).json()
        assert status["status"] == "failed" and status["attempts"] == 3
        assert (await client.post("/v1/jobs/" + job_id + "/retry")).status_code == 202
        assert await process_one(store, provider)
        assert (await client.get("/v1/jobs/" + job_id)).json()["status"] == "completed"
        result = await client.post("/v1/search", json={"query": "cedar"})
        assert result.status_code == 200 and result.json()["items"]
        assert (await client.get("/v1/jobs/missing")).status_code == 404
        changed = doc.model_copy(update={"text": "different"})
        client.headers["Idempotency-Key"] = "changed"
        assert (
            await client.put(
                "/v1/documents/" + doc.documentId, json=changed.model_dump(mode="json")
            )
        ).status_code == 409


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


async def test_developer_career_question_matches_engineer_profiles(store, doc, access):
    profiles = doc.model_copy(
        update={
            "documentId": "engineering",
            "title": "Профили инженеров",
            "text": "# E3\nСамостоятельно решает задачи в своей команде.",
        }
    )
    bi = doc.model_copy(
        update={
            "documentId": "bi",
            "title": "BI разработчики",
            "text": "# Оценка проекта\nОценивают уровень вклада разработчика в BI-проект.",
        }
    )
    await store.publish([profiles, bi], DeterministicProvider())
    result = await store.search("Как оценивают уровень разработчика?", access, None, 5, 0.5)
    assert result.items[0].documentId == "engineering"
    unrelated = await store.search("Что делает разработчик в BI-проекте?", access, None, 5, 0.5)
    assert {item.documentId for item in unrelated.items} == {"bi"}
    specialized = await store.search("Как оценивают уровень BI-разработчика?", access, None, 5, 0.5)
    assert {item.documentId for item in specialized.items} == {"bi"}


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
