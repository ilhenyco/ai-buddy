import httpx
import pytest
from mcp import ClientSession
from mcp.client.streamable_http import streamable_http_client

from homych_knowledge.app import create_app


async def test_http_auth_and_strict_input(settings, service):
    app = create_app(settings, service)
    async with app.router.lifespan_context(app):
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=app), base_url="http://localhost"
        ) as client:
            assert (await client.get("/healthz")).status_code == 200
            body = {"subjectId": "u-sasha", "query": "доступ"}
            assert (await client.post("/internal/v1/search", json=body)).status_code == 401
            client.headers["Authorization"] = f"Bearer {settings.service_token}"
            assert (await client.post("/internal/v1/search", json=body)).status_code == 200
            assert (
                await client.post("/internal/v1/search", json={**body, "role": "lead"})
            ).status_code == 422
            assert (await client.get("/internal/metrics")).status_code == 200
            assert (await client.post("/mcp/", json={})).status_code == 401


async def test_real_mcp_initialize_discover_call_matches_http(settings, service):
    app = create_app(settings, service)
    async with app.router.lifespan_context(app):
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=app),
            base_url="http://localhost:8090",
            headers={"Authorization": "Bearer test-mcp-token-at-least-24"},
        ) as client:
            async with streamable_http_client(
                "http://localhost:8090/mcp/", http_client=client
            ) as streams:
                async with ClientSession(streams[0], streams[1]) as session:
                    await session.initialize()
                    tools = await session.list_tools()
                    assert [tool.name for tool in tools.tools] == ["search_knowledge"]
                    tool = tools.tools[0]
                    assert "subjectId" not in tool.inputSchema["properties"]
                    result = await session.call_tool("search_knowledge", {"query": "доступ"})
                    assert not result.isError
                    assert (
                        result.structuredContent == service.store.search.return_value.model_dump()
                    )
                    invalid = await session.call_tool(
                        "search_knowledge", {"query": "доступ", "limit": 99}
                    )
                    assert invalid.isError
            # Same principal, query, and retrieval output through the HTTP adapter.
            client.headers["Authorization"] = f"Bearer {settings.service_token}"
            response = await client.post(
                "/internal/v1/search", json={"subjectId": "u-sasha", "query": "доступ"}
            )
            assert response.json() == result.structuredContent


@pytest.mark.parametrize("status", [401, 403, 404])
async def test_identity_denial_is_safe(settings, service, status):
    from homych_knowledge.service import IdentityError

    service.identity.resolve.side_effect = IdentityError(status)
    app = create_app(settings, service)
    async with app.router.lifespan_context(app):
        async with httpx.AsyncClient(
            transport=httpx.ASGITransport(app=app),
            base_url="http://localhost",
            headers={"Authorization": f"Bearer {settings.service_token}"},
        ) as client:
            response = await client.post(
                "/internal/v1/search", json={"subjectId": "u-sasha", "query": "доступ"}
            )
            assert response.status_code == status
            assert response.json() == {"code": "ACCESS_DENIED"}
