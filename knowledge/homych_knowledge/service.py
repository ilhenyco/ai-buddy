import asyncio
import json
import logging
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

log = logging.getLogger(__name__)


class ModelReply(StrictModel):
    answered: bool = True
    reply: str = Field(min_length=1, max_length=6000)
    sourceIds: list[str] = Field(max_length=5)
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


CONTEXT_CHARS = 1000


def build_prompt(question: str, context: str | None, hits: list, budget: int):
    # The stage message is background: it must not crowd excerpts out of the budget.
    if context and len(context) > CONTEXT_CHARS:
        context = context[:CONTEXT_CHARS].rstrip() + "…"
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
                # Metadata only: no question, excerpt or reply text in logs.
                log.info(
                    "answer request=%s mode=%s excerpts=%s answered=%s cited=%s matched=%s",
                    request.requestId,
                    result.retrievalMode,
                    [h.chunkId for h in selected],
                    generated.answered,
                    len(generated.sourceIds),
                    len({h.chunkId for h in selected} & set(generated.sourceIds)),
                )
                if generated.conflict:
                    return fallback("conflicting_sources")
                # The model found no answer in the excerpts: honest fallback, no sources.
                if not generated.answered:
                    return fallback("no_evidence")
                # Links come only from citations: a URL in the text is removed, not trusted.
                reply = re.sub(r"(https?://|www\.)\S+", "", generated.reply).strip()
                if not reply:
                    return fallback("unavailable")
                by_id = {h.chunkId: h for h in selected}
                # Only excerpts that were actually given count as sources. A reply that cites none
                # of them is not grounded: the honest outcome is "no evidence", not an outage.
                ids = [key for key in dict.fromkeys(generated.sourceIds) if key in by_id]
                if not ids:
                    return fallback("no_evidence")
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
                    reply=reply,
                    citations=citations,
                    snapshotId=result.snapshotId,
                    retrievalMode=result.retrievalMode,
                )
        except (ProviderError, ValidationError, TimeoutError):
            return fallback("unavailable")
