from datetime import UTC, datetime
from unittest.mock import AsyncMock

import pytest

from homych_knowledge.config import Settings
from homych_knowledge.models import AccessContext, Document, Hit, SearchResult
from homych_knowledge.provider import DeterministicProvider
from homych_knowledge.service import KnowledgeService


@pytest.fixture
def settings():
    return Settings(
        service_token="test-service-token-at-least-24",
        demo_mode=True,
        provider="deterministic",
        mcp_tokens={"test-mcp-token-at-least-24": "u-sasha"},
    )


@pytest.fixture
def doc():
    return Document(
        documentId="test-doc",
        revision="v1",
        title="Инструкция",
        text="# Начало\nПлатформенный секрет cedar.\n\n## Доступ\nОбратись к бадди.",
        url="https://example.invalid/source",
        validFrom=datetime(2026, 1, 1, tzinfo=UTC),
        expiresAt=datetime(2100, 1, 1, tzinfo=UTC),
    )


@pytest.fixture
def access():
    return AccessContext(
        subjectId="u-sasha", role="intern", teamIds=["platform"], internshipIds=["assignment-sasha"]
    )


@pytest.fixture
def service(settings, access):
    hit = Hit(
        chunkId="c1",
        documentId="d1",
        revision="v1",
        title="Инструкция",
        section="Доступ",
        url="https://example.invalid/source",
        synthetic=False,
        text="Спроси у бадди.",
        score=0.1,
    )
    store = AsyncMock()
    store.active_digest.return_value = "deterministic-1024-v1"
    store.search.return_value = SearchResult(snapshotId="s1", items=[hit])
    store.permitted.return_value = True
    identity = AsyncMock()
    identity.resolve.return_value = access
    return KnowledgeService(settings, store, DeterministicProvider(), identity)
