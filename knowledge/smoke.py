"""Run inside the knowledge container after publishing the real Playbook corpus."""

import asyncio
import os

import httpx
from mcp import ClientSession
from mcp.client.streamable_http import streamable_http_client


async def main():
    token = os.environ["RAG_SERVICE_TOKEN"]
    async with httpx.AsyncClient(
        base_url="http://localhost:8090", timeout=65, headers={"Authorization": f"Bearer {token}"}
    ) as client:
        response = await client.get("/readyz")
        response.raise_for_status()
        query = "Как устроено обучение и развитие?"
        payload = {"subjectId": "u-sasha", "query": query}
        result = await client.post("/internal/v1/search", json=payload)
        result.raise_for_status()
        search = result.json()
        assert search["items"] and all(not item["synthetic"] for item in search["items"])
        answer = await client.post("/internal/v1/answer", json={**payload, "requestId": "ci-smoke"})
        answer.raise_for_status()
        assert answer.json()["status"] == "answered", answer.json()
        assert answer.json()["citations"]
        denied = await client.post("/internal/v1/search", json={**payload, "subjectId": "u-alex"})
        assert denied.status_code == 403
        client.headers["Authorization"] = "Bearer ci-mcp-token-with-at-least-24-characters"
        async with streamable_http_client(
            "http://localhost:8090/mcp/", http_client=client
        ) as streams:
            async with ClientSession(streams[0], streams[1]) as session:
                await session.initialize()
                result = await session.call_tool("search_knowledge", {"query": query})
                assert not result.isError
                assert result.structuredContent == search
    print("Smoke passed: Go identity + real Playbook SQL search + answer + network MCP")


if __name__ == "__main__":
    asyncio.run(main())
