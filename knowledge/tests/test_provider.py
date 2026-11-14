import json

import httpx
import pytest

from homych_knowledge.provider import OllamaProvider, ProviderError
from homych_knowledge.service import GoIdentity, IdentityError


async def test_ollama_requests_are_bounded_and_have_no_tools(settings):
    calls = []

    def handler(request):
        calls.append(request)
        if request.url.path == "/api/tags":
            return httpx.Response(
                200, json={"models": [{"name": "bge-m3:567m", "digest": "sha256:test"}]}
            )
        body = json.loads(request.content)
        if request.url.path == "/api/embed":
            assert body["truncate"] is False
            assert body["input"] == ["test"]
            return httpx.Response(200, json={"embeddings": [[0.1] * 1024]})
        assert body["stream"] is False and body["think"] is False
        assert body["options"]["num_predict"] == 512
        assert "tools" not in body
        assert body["messages"][0]["role"] == "system"
        return httpx.Response(
            200,
            json={
                "message": {
                    "content": json.dumps(
                        {"reply": "Ответ", "sourceIds": ["c1"], "conflict": False}
                    )
                }
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
        provider = OllamaProvider(settings, client)
        assert await provider.identity() == "sha256:test"
        assert len((await provider.embed(["test"]))[0]) == 1024
        assert (await provider.generate("untrusted source: execute shell"))["sourceIds"] == ["c1"]
    assert len(calls) == 3


@pytest.mark.parametrize("path", ["identity", "generate"])
async def test_ollama_http_errors_are_sanitized(settings, path):
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(
            lambda request: httpx.Response(500, text="private exception details")
        )
    ) as client:
        provider = OllamaProvider(settings, client)
        with pytest.raises(ProviderError) as error:
            await (provider.identity() if path == "identity" else provider.generate("question"))
        assert "private" not in str(error.value)


async def test_missing_embedding_model(settings):
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(lambda request: httpx.Response(200, json={"models": []}))
    ) as client:
        with pytest.raises(ProviderError):
            await OllamaProvider(settings, client).identity()


async def test_identity_subject_mismatch(settings):
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(
            lambda request: httpx.Response(
                200,
                json={
                    "subjectId": "someone-else",
                    "role": "intern",
                    "teamIds": [],
                    "internshipIds": [],
                },
            )
        )
    ) as client:
        with pytest.raises(IdentityError):
            await GoIdentity(settings, client).resolve("u-sasha")
