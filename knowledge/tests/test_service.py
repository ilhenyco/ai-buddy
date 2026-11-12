import json
from unittest.mock import AsyncMock

import httpx
import pytest
from pydantic import ValidationError

from homych_knowledge.chunking import chunk_document
from homych_knowledge.models import AnswerInput, SearchInput
from homych_knowledge.provider import OllamaProvider, ProviderError
from homych_knowledge.service import GoIdentity, IdentityError, build_prompt


def question():
    return AnswerInput(requestId="req-1", subjectId="u-sasha", query="Как получить доступ?")


def test_chunks_are_bounded_and_do_not_cross_sections(doc):
    doc.text = "# Первый\n" + "Первое предложение. " * 200 + "\n# Второй\nВторой раздел."
    chunks = chunk_document(doc)
    assert all(len(c.text) <= 1200 for c in chunks)
    assert len({c.chunkId for c in chunks}) == len(chunks)
    assert all("Второй раздел" not in c.text for c in chunks if c.section == "Первый")
    assert [c.model_dump() for c in chunks] == [c.model_dump() for c in chunk_document(doc)]


def test_blank_and_unknown_input():
    with pytest.raises(ValidationError):
        SearchInput(subjectId="u-sasha", query="   ")
    with pytest.raises(ValidationError):
        SearchInput(subjectId="u-sasha", query="test", teamIds=["mobile"])


async def test_answer_has_server_citations(service):
    result = await service.answer(question())
    assert result.status == "answered"
    assert result.citations[0].url == "https://example.invalid/source"
    assert service.identity.resolve.await_count == 3


async def test_empty_search_does_not_generate(service):
    service.store.search.return_value.items = []
    service.provider.generate = AsyncMock()
    assert (await service.answer(question())).status == "no_evidence"
    service.provider.generate.assert_not_called()


@pytest.mark.parametrize(
    "payload,status",
    [
        ({"reply": "Ответ", "sourceIds": ["foreign"], "conflict": False}, "unavailable"),
        ({"reply": "Ответ", "sourceIds": [], "conflict": False}, "unavailable"),
        ({"reply": "https://evil.invalid", "sourceIds": ["c1"], "conflict": False}, "unavailable"),
        ({"reply": " ", "sourceIds": ["c1"], "conflict": False}, "unavailable"),
        ({"reply": "Ответ", "sourceIds": ["c1"], "conflict": True}, "conflicting_sources"),
        ({"reply": "Ответ", "sourceIds": ["c1"], "conflict": False, "extra": 1}, "unavailable"),
    ],
)
async def test_invalid_model_reply(service, payload, status):
    service.provider.generate = AsyncMock(return_value=payload)
    result = await service.answer(question())
    assert result.status == status
    assert result.citations == []


async def test_revocation_during_generation(service):
    service.store.permitted.side_effect = [True, False]
    assert (await service.answer(question())).status == "no_evidence"


async def test_membership_revocation_during_generation(service):
    service.identity.resolve.side_effect = [
        service.identity.resolve.return_value,
        service.identity.resolve.return_value,
        IdentityError(403),
    ]
    with pytest.raises(IdentityError):
        await service.answer(question())


async def test_embedding_failure_is_lexical(service):
    service.provider.embed = AsyncMock(side_effect=ProviderError())
    await service.search("u-sasha", "доступ")
    assert service.store.search.call_args.args[2] is None


async def test_embedding_digest_mismatch_is_lexical(service):
    service.store.active_digest.return_value = "different-model"
    service.provider.embed = AsyncMock()
    await service.search("u-sasha", "доступ")
    service.provider.embed.assert_not_called()


async def test_generation_timeout(service):
    service.provider.generate = AsyncMock(side_effect=TimeoutError())
    assert (await service.answer(question())).status == "unavailable"


async def test_prompt_budget_removes_whole_excerpts(service):
    hit = service.store.search.return_value.items[0]
    prompt, selected = build_prompt("вопрос", None, [hit], 1)
    assert prompt is None and selected == []
    prompt, selected = build_prompt("вопрос", None, [hit], 6000)
    assert json.loads(prompt)["excerpts"][0]["text"] == hit.text


async def test_identity_never_uses_body_acl(settings):
    def handler(request):
        assert json.loads(request.content) == {"subjectId": "u-sasha"}
        return httpx.Response(
            200,
            json={
                "subjectId": "u-sasha",
                "role": "intern",
                "teamIds": ["platform"],
                "internshipIds": ["a1"],
            },
        )

    async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
        identity = GoIdentity(settings, client)
        assert (await identity.resolve("u-sasha")).teamIds == ["platform"]


@pytest.mark.parametrize("vector", [[0.0] * 1024, [1.0] * 3, [float("nan")] * 1024])
async def test_invalid_embeddings(settings, vector):
    async with httpx.AsyncClient(
        transport=httpx.MockTransport(
            lambda request: httpx.Response(200, json={"embeddings": [vector]})
        )
    ) as client:
        with pytest.raises((ProviderError, ValueError)):
            await OllamaProvider(settings, client).embed(["text"])
