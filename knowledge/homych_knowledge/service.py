import asyncio
import json
import re
from typing import Literal

from pydantic import Field, ValidationError

from .config import Settings
from .models import (
    AccessContext,
    AnswerInput,
    AnswerResult,
    Citation,
    SearchResult,
    StrictModel,
)
from .provider import SYSTEM, ProviderError


class ModelReply(StrictModel):
    reply: str = Field(min_length=1, max_length=6000)
    sourceIds: list[str] = Field(min_length=1, max_length=5)
    conflict: bool


class IdentityError(Exception):
    def __init__(self, status: int):
        self.status = status


class GoIdentity:
    def __init__(self, settings: Settings, client):
        self.settings, self.client = settings, client

    async def resolve(self, subject_id: str) -> AccessContext:
        if subject_id.startswith("service:"):
            client = self.settings.external_clients.get(subject_id.removeprefix("service:"))
            if client is None:
                raise IdentityError(403)
            return AccessContext(
                subjectId=subject_id, role="service", teamIds=client.team_ids, internshipIds=[]
            )
        response = await self.client.post(
            f"{self.settings.go_url}/internal/v1/ai-context",
            headers={"Authorization": f"Bearer {self.settings.service_token}"},
            json={"subjectId": subject_id},
            timeout=5,
        )
        if response.status_code in (401, 403, 404):
            raise IdentityError(response.status_code)
        response.raise_for_status()
        access = AccessContext.model_validate(response.json())
        if access.subjectId != subject_id:
            raise IdentityError(403)
        access.synthetic = (
            self.settings.demo_mode and subject_id in self.settings.synthetic_subjects
        )
        return access


def build_prompt(question: str, context: str | None, hits: list, budget: int):
    selected = list(hits)
    while selected:
        prompt = json.dumps(
            {
                "question": question,
                "messageContext": context,
                "excerpts": [
                    {
                        "id": h.chunkId,
                        "title": h.title,
                        "section": h.section,
                        "synthetic": h.synthetic,
                        "sourceType": "public_playbook"
                        if h.url.startswith("https://github.com/avito-tech/playbook/")
                        else "knowledge",
                        "text": h.text,
                    }
                    for h in selected
                ],
            },
            ensure_ascii=False,
        )
        if len((SYSTEM + prompt).encode()) <= budget:
            return prompt, selected
        selected.pop()
    return None, []


class KnowledgeService:
    def __init__(self, settings: Settings, store, provider, identity):
        self.settings, self.store, self.provider, self.identity = (
            settings,
            store,
            provider,
            identity,
        )

    async def search(self, subject_id: str, query: str, limit: int = 5) -> SearchResult:
        access = await self.identity.resolve(subject_id)
        vector = None
        digest = None
        try:
            digest = await self.provider.identity()
            if digest == await self.store.active_digest():
                vector = (await self.provider.embed([query]))[0]
        except ProviderError:
            pass
        result = await self.store.search(
            query, access, vector, limit, self.settings.min_similarity, digest
        )
        if result.items:
            # Re-resolve memberships as well as source revocations after ranking.
            current = await self.identity.resolve(subject_id)
            if not await self.store.permitted(
                current, result.snapshotId, [h.chunkId for h in result.items]
            ):
                result.items = []
        return result

    async def answer(self, request: AnswerInput) -> AnswerResult:
        result = None

        def fallback(status: Literal["no_evidence", "conflicting_sources", "unavailable"]):
            replies = {
                "no_evidence": "В доступных материалах нет подтверждённого ответа. Уточни у бадди.",
                "conflicting_sources": (
                    "В материалах есть противоречие. Уточни актуальное правило у бадди."
                ),
                "unavailable": "Сейчас не получается подготовить ответ. Попробуй позже.",
            }
            return AnswerResult(
                requestId=request.requestId,
                status=status,
                reply=replies[status],
                snapshotId=result.snapshotId if result else None,
                retrievalMode=result.retrievalMode if result else "hybrid",
            )

        try:
            async with asyncio.timeout(self.settings.request_timeout):
                result = await self.search(request.subjectId, request.query, request.limit)
                prompt, selected = build_prompt(
                    request.query, request.messageContext, result.items, self.settings.prompt_bytes
                )
                if not selected:
                    return fallback("no_evidence")
                generated = ModelReply.model_validate(await self.provider.generate(prompt))
                if generated.conflict:
                    return fallback("conflicting_sources")
                if not generated.reply.strip() or re.search(r"https?://|www\.", generated.reply):
                    return fallback("unavailable")
                by_id = {h.chunkId: h for h in selected}
                if any(key not in by_id for key in generated.sourceIds):
                    return fallback("unavailable")
                ids = list(dict.fromkeys(generated.sourceIds))
                access = await self.identity.resolve(request.subjectId)
                if not await self.store.permitted(access, result.snapshotId, ids):
                    return fallback("no_evidence")
                citations = [
                    Citation.model_validate(by_id[key].model_dump(exclude={"text", "score"}))
                    for key in ids
                ]
                return AnswerResult(
                    requestId=request.requestId,
                    status="answered",
                    reply=generated.reply,
                    citations=citations,
                    snapshotId=result.snapshotId,
                    retrievalMode=result.retrievalMode,
                )
        except (ProviderError, ValidationError, TimeoutError):
            return fallback("unavailable")
