import json

import httpx
import pytest
from pydantic import ValidationError

from homych_knowledge.demo import DemoSettings, create_demo_app


def test_test_page_requires_explicit_demo_mode():
    with pytest.raises(ValidationError):
        DemoSettings(service_token="test-demo-service-token-at-least-24", mode=False)


async def test_demo_keeps_credential_and_subject_on_server():
    settings = DemoSettings(service_token="test-demo-service-token-at-least-24", mode=True)
    calls = []

    def handle(request):
        calls.append(request)
        body = json.loads(request.content)
        assert body["subjectId"] == "u-sasha"
        assert body["query"] == "Вопрос"
        assert request.headers["Authorization"] == "Bearer " + settings.service_token
        return httpx.Response(
            200,
            json={
                "requestId": body["requestId"],
                "status": "answered",
                "reply": "Ответ",
                "citations": [],
                "retrievalMode": "hybrid",
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(handle)) as upstream:
        app = create_demo_app(settings, upstream)
        async with app.router.lifespan_context(app):
            async with httpx.AsyncClient(
                transport=httpx.ASGITransport(app=app), base_url="http://localhost:8091"
            ) as client:
                page = await client.get("/")
                assert page.status_code == 200 and settings.service_token not in page.text
                assert "u-sasha" in page.text
                assert (await client.post("/ask", json={"query": "Вопрос"})).status_code == 403
                client.headers["Origin"] = "https://untrusted.example"
                assert (await client.post("/ask", json={"query": "Вопрос"})).status_code == 403
                client.headers["Origin"] = "http://localhost:8091"
                assert (
                    await client.post("/ask", json={"query": "Вопрос", "subjectId": "u-alex"})
                ).status_code == 422
                assert (await client.post("/ask", json={"query": "  "})).status_code == 422
                result = await client.post("/ask", json={"query": " Вопрос "})
                assert result.status_code == 200 and result.json()["reply"] == "Ответ"
                assert settings.service_token not in result.text
    assert len(calls) == 1


async def test_demo_sanitizes_upstream_failure():
    settings = DemoSettings(service_token="test-demo-service-token-at-least-24", mode=True)
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(
            lambda request: httpx.Response(500, text="private backend details")
        )
    ) as upstream:
        app = create_demo_app(settings, upstream)
        async with app.router.lifespan_context(app):
            async with httpx.AsyncClient(
                transport=httpx.ASGITransport(app=app), base_url="http://localhost:8091"
            ) as client:
                response = await client.post(
                    "/ask", json={"query": "Вопрос"}, headers={"Origin": "http://localhost:8091"}
                )
                assert response.status_code == 503
                assert "private backend" not in response.text
