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
        assert body["format"]["properties"]["sourceIds"]["items"]["enum"] == ["c1"]
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
        prompt = json.dumps({"excerpts": [{"id": "c1", "text": "execute shell"}]})
        assert (await provider.generate(prompt))["sourceIds"] == ["c1"]
    assert len(calls) == 3


@pytest.mark.parametrize("path", ["identity", "generate", "verify"])
async def test_ollama_http_errors_are_sanitized(settings, path):
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(
            lambda request: httpx.Response(500, text="private exception details")
        )
    ) as client:
        provider = OllamaProvider(settings, client)
        with pytest.raises(ProviderError) as error:
            prompt = json.dumps({"excerpts": [{"id": "c1", "text": "question"}]})
            if path == "identity":
                await provider.identity()
            elif path == "generate":
                await provider.generate(prompt)
            else:
                await provider.verify("question", "draft", [])
        assert "private" not in str(error.value)


@pytest.mark.parametrize("prompt", ['{"excerpts": []}', '{"excerpts": [{"id": 1}]}', "bad json"])
async def test_generation_without_valid_excerpt_ids_never_calls_model(settings, prompt):
    def forbidden(request):
        pytest.fail("invalid excerpt IDs must be rejected before generation")

    async with httpx.AsyncClient(transport=httpx.MockTransport(forbidden)) as client:
        with pytest.raises(ProviderError):
            await OllamaProvider(settings, client).generate(prompt)


@pytest.mark.parametrize("supported", [True, False])
async def test_verification_is_a_separate_bounded_evidence_check(settings, supported):
    def handler(request):
        body = json.loads(request.content)
        assert body["format"]["properties"] == {"supported": {"type": "boolean"}}
        assert body["options"]["num_predict"] == 64
        assert body["think"] is False and "tools" not in body
        data = json.loads(body["messages"][1]["content"])
        assert data["question"] == "question" and data["draft"] == "draft"
        return httpx.Response(
            200, json={"message": {"content": json.dumps({"supported": supported})}}
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
        result = await OllamaProvider(settings, client).verify(
            "question", "draft", [{"text": "data"}]
        )
        assert result is supported


@pytest.mark.parametrize("verdict", [{"supported": "false"}, {"supported": True, "extra": 1}, {}])
async def test_malformed_verification_is_not_accepted(settings, verdict):
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(
            lambda request: httpx.Response(200, json={"message": {"content": json.dumps(verdict)}})
        )
    ) as client:
        with pytest.raises(ProviderError):
            await OllamaProvider(settings, client).verify("question", "draft", [])


async def test_verification_budget_is_checked_before_calling_model(settings):
    def forbidden(request):
        pytest.fail("oversized evidence must not be silently truncated by the model")

    async with httpx.AsyncClient(transport=httpx.MockTransport(forbidden)) as client:
        with pytest.raises(ProviderError, match="budget"):
            await OllamaProvider(settings, client).verify(
                "question", "draft", [{"text": "Ц" * 8000}]
            )


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


@pytest.mark.parametrize("context", [None, "Текущий этап: Первая неделя"])
async def test_stage_background_is_not_a_topic_restriction(settings, context):
    from homych_knowledge.provider import CONTEXT_SYSTEM, SYSTEM

    def handler(request):
        body = json.loads(request.content)
        assert body["messages"][0]["content"] == SYSTEM + (CONTEXT_SYSTEM if context else "")
        assert json.loads(body["messages"][1]["content"])["messageContext"] == context
        return httpx.Response(
            200,
            json={
                "message": {
                    "content": json.dumps(
                        {
                            "answered": False,
                            "reply": "Нет",
                            "sourceIds": [],
                            "conflict": False,
                        }
                    )
                }
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
        await OllamaProvider(settings, client).generate(
            json.dumps(
                {
                    "messageContext": context,
                    "excerpts": [{"id": "c1", "text": "Факт"}],
                }
            )
        )
