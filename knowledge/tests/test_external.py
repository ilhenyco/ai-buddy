import hashlib
from datetime import UTC, datetime

import httpx
import pytest
from mcp import ClientSession
from mcp.client.streamable_http import streamable_http_client

from homych_knowledge.app import create_app
from homych_knowledge.config import ExternalClient, Settings
from homych_knowledge.service import GoIdentity

TOKEN = "external-client-test-credential-with-entropy"


def client_config(scopes, expires="2030-01-01T00:00:00Z"):
    return ExternalClient(
        token_sha256=hashlib.sha256(TOKEN.encode()).hexdigest(),
        scopes=scopes,
        team_ids=["platform"],
        expires_at=expires,
    )


async def test_service_identity_without_go():
    settings = Settings(
        service_token="internal-service-secret-at-least-24",
        external_clients={"avito": client_config(["knowledge:search"])},
    )
    # There is deliberately no HTTP client or Go backend.
    context = await GoIdentity(settings, None).resolve("service:avito")
    assert context.role == "service"
    assert context.teamIds == ["platform"]
    assert not context.internshipIds and not context.synthetic


async def test_external_mcp_and_scopes(settings, service):
    settings.external_clients = {"avito": client_config(["knowledge:search"])}
    app = create_app(settings, service)
    async with app.router.lifespan_context(app):
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=app),
            base_url="http://localhost:8090",
            headers={"Authorization": "Bearer " + TOKEN},
        ) as client:
            async with streamable_http_client(
                "http://localhost:8090/mcp/", http_client=client
            ) as streams:
                async with ClientSession(streams[0], streams[1]) as session:
                    await session.initialize()
                    tools = await session.list_tools()
                    assert all(tool.annotations.readOnlyHint for tool in tools.tools)
                    result = await session.call_tool("search_knowledge", {"query": "доступ"})
                    assert not result.isError
                    service.identity.resolve.assert_awaited_with("service:avito")
                    denied = await session.call_tool(
                        "get_knowledge_document", {"documentId": "d1", "revision": "v1"}
                    )
                    assert denied.isError
                    settings.external_clients["avito"].scopes.append("knowledge:read")
                    service.store.document.return_value = service.store.search.return_value
                    source = await session.call_tool(
                        "get_knowledge_document", {"documentId": "d1", "revision": "v1"}
                    )
                    assert not source.isError
                    assert source.structuredContent["items"]
            assert (await client.delete("/v1/documents/d1")).status_code == 403
            assert (await client.post("/v1/search", json={"query": "доступ"})).status_code == 200
            settings.external_clients["avito"].expires_at = datetime(2020, 1, 1, tzinfo=UTC)
            assert (await client.post("/v1/search", json={"query": "доступ"})).status_code == 401


@pytest.mark.parametrize("scope", [[], ["documents:write"]])
async def test_writer_cannot_search(settings, service, scope):
    settings.external_clients = {"writer": client_config(scope)}
    app = create_app(settings, service)
    async with httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app),
        base_url="http://localhost",
        headers={"Authorization": "Bearer " + TOKEN},
    ) as client:
        assert (await client.post("/mcp/", json={})).status_code == 403


async def test_document_input_boundaries(settings, service):
    settings.external_clients = {"writer": client_config(["documents:write"])}
    app = create_app(settings, service)
    async with httpx.AsyncClient(
        transport=httpx.ASGITransport(app=app),
        base_url="http://localhost",
        headers={"Authorization": "Bearer " + TOKEN},
    ) as client:
        assert (await client.put("/v1/documents/d1", content=b"x" * 1_000_001)).status_code == 413
        assert (await client.put("/v1/documents/d1", json={})).status_code == 422
