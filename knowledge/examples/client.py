"""Run: MCP_URL=https://host/mcp/ MCP_TOKEN=... python examples/client.py"""

import asyncio
import os

import httpx
from mcp import ClientSession
from mcp.client.streamable_http import streamable_http_client


async def main():
    async with httpx.AsyncClient(
        headers={"Authorization": "Bearer " + os.environ["MCP_TOKEN"]}
    ) as client:
        async with streamable_http_client(os.environ["MCP_URL"], http_client=client) as streams:
            async with ClientSession(streams[0], streams[1]) as session:
                await session.initialize()
                print(await session.list_tools())
                result = await session.call_tool(
                    "search_knowledge", {"query": "Как устроено обучение?"}
                )
                print(result.structuredContent)


asyncio.run(main())
