"""Evaluate retrieval and optionally live answers; never index this question set."""

import argparse
import asyncio
import hashlib
import json
import logging
import sys
import time
from collections import Counter
from pathlib import Path

import httpx

from .config import Settings
from .models import AnswerInput
from .provider import (
    GENERATION_OPTIONS,
    SYSTEM,
    VERIFICATION_OPTIONS,
    VERIFY_SYSTEM,
    DeterministicProvider,
    OllamaProvider,
)
from .runtime import loop_factory
from .service import GoIdentity, KnowledgeService
from .store import Store


class RecordingProvider:
    def __init__(self, provider):
        self.provider = provider
        self.prompt = None
        self.generated = None
        self.verified = None

    async def identity(self):
        return await self.provider.identity()

    async def embed(self, texts):
        return await self.provider.embed(texts)

    async def generate(self, prompt):
        self.prompt = json.loads(prompt)
        self.generated = await self.provider.generate(prompt)
        return self.generated

    async def verify(self, question, reply, sources):
        self.verified = await self.provider.verify(question, reply, sources)
        return self.verified


def diagnose(row):
    """Mechanical attribution; semantic correctness still requires review."""
    if row["status"] == "unavailable":
        return "unavailable"
    if not row["answerable"]:
        if row["status"] == "no_evidence":
            return "correct_abstention"
        return "false_answer" if row["status"] == "answered" else "unexpected_outcome"
    if not row["hit"]:
        return "retrieval_miss"
    if not row["expectedInPrompt"]:
        return "prompt_miss"
    if row["generated"] and row["generated"].get("answered") is False:
        return "model_abstention"
    if row.get("verified") is False:
        return "verification_rejected"
    if row["status"] != "answered":
        return "answer_rejected"
    return "answered_with_expected_source" if row["citesExpected"] else "wrong_source"


def summarize(rows, generation):
    positive = [r for r in rows if r["answerable"]]
    negative = [r for r in rows if not r["answerable"]]
    result = {
        "count": len(rows),
        "answerable": len(positive),
        "unanswerable": len(negative),
        "recallAt5": sum(r["hit"] for r in positive) / len(positive) if positive else None,
        "modes": dict(Counter(r["mode"] for r in rows)),
    }
    if generation:
        result.update(
            positiveStatuses=dict(Counter(r["status"] for r in positive)),
            negativeStatuses=dict(Counter(r["status"] for r in negative)),
            diagnoses=dict(Counter(r["diagnosis"] for r in rows)),
            answeredWithExpectedSource=sum(r["citesExpected"] for r in positive),
            falseAnswers=sum(r["status"] == "answered" for r in negative),
            correctAbstentions=sum(r["status"] == "no_evidence" for r in negative),
        )
    return result


def write_report(path, report):
    output = Path(path)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")


async def evaluate(args):
    settings = Settings()
    cases = json.loads(await asyncio.to_thread(Path(args.cases).read_text, encoding="utf-8"))
    cases = [c for c in cases if args.split == "all" or c["split"] == args.split]
    if args.ids:
        cases = [c for c in cases if c["id"] in args.ids.split(",")]
    if not cases:
        raise ValueError("empty evaluation split")
    async with httpx.AsyncClient(timeout=15) as client:
        base = (
            DeterministicProvider()
            if settings.provider == "deterministic"
            else OllamaProvider(settings, client)
        )
        provider = RecordingProvider(base)
        store = Store(settings.database_url)
        service = KnowledgeService(settings, store, provider, GoIdentity(settings, client))
        rows = []
        report = {
            "split": args.split,
            "generation": args.generation,
            "provider": settings.provider,
            "generationModel": settings.generation_model,
            "embeddingDigest": await store.active_digest(),
            "systemSha256": hashlib.sha256(SYSTEM.encode()).hexdigest(),
            "verificationSha256": hashlib.sha256(VERIFY_SYSTEM.encode()).hexdigest(),
            "generationOptions": GENERATION_OPTIONS,
            "verificationOptions": VERIFICATION_OPTIONS,
            "settings": {
                "minSimilarity": settings.min_similarity,
                "limit": 5,
                "promptBytes": settings.prompt_bytes,
                "generationTimeout": settings.generation_timeout,
                "requestTimeout": settings.request_timeout,
            },
            "caseSetSha256": hashlib.sha256(
                json.dumps(cases, ensure_ascii=False, sort_keys=True).encode()
            ).hexdigest(),
            "results": rows,
        }
        if settings.provider == "ollama":
            tags = await client.get(f"{settings.ollama_url}/api/tags")
            tags.raise_for_status()
            report["modelDigests"] = {m["name"]: m["digest"] for m in tags.json()["models"]}
        for case in cases:
            start = time.monotonic()
            result = await service.search(args.subject, case["query"])
            path = case.get("path")
            expected = "avito-" + hashlib.sha256(path.encode()).hexdigest()[:24] if path else None
            row = {
                "id": case["id"],
                "split": case["split"],
                "query": case["query"],
                "path": path,
                "answerable": case.get("answerable", True),
                "hit": any(h.documentId == expected for h in result.items) if expected else None,
                "retrievalSeconds": round(time.monotonic() - start, 3),
                "mode": result.retrievalMode,
                "snapshotId": result.snapshotId,
                "hits": [h.model_dump() for h in result.items],
            }
            if args.generation:
                provider.prompt = provider.generated = provider.verified = None
                answer = await service.answer(
                    AnswerInput(
                        requestId="eval-" + case["id"], subjectId=args.subject, query=case["query"]
                    )
                )
                excerpts = (provider.prompt or {}).get("excerpts", [])
                expected_ids = {h.chunkId for h in result.items if h.documentId == expected}
                row.update(
                    status=answer.status,
                    reply=answer.reply,
                    citations=[c.model_dump() for c in answer.citations],
                    excerpts=excerpts,
                    generated=provider.generated,
                    verified=provider.verified,
                    expectedInPrompt=any(e["id"] in expected_ids for e in excerpts),
                    citesExpected=answer.status == "answered"
                    and any(c.documentId == expected for c in answer.citations),
                )
                row["diagnosis"] = diagnose(row)
            row["seconds"] = round(time.monotonic() - start, 3)
            rows.append(row)
            report.update(summarize(rows, args.generation))
            report["complete"] = len(rows) == len(cases)
            if args.output:
                await asyncio.to_thread(write_report, args.output, report)
            print(
                f"{len(rows)}/{len(cases)} {case['id']} hit={row['hit']} "
                f"{row.get('diagnosis', '')} {row['seconds']}s",
                file=sys.stderr,
                flush=True,
            )
        print(json.dumps(report, ensure_ascii=False, indent=2))
        return report["recallAt5"] is None or report["recallAt5"] >= args.minimum_recall


def main():
    logging.basicConfig(level=logging.INFO, format="%(message)s")
    logging.getLogger("httpx").setLevel(logging.WARNING)
    parser = argparse.ArgumentParser()
    parser.add_argument("--cases", default="data/eval.json")
    parser.add_argument("--split", choices=["dev", "test", "all"], default="test")
    parser.add_argument("--ids", help="Comma-separated IDs for a focused diagnostic run")
    parser.add_argument("--subject", default="u-sasha")
    parser.add_argument("--generation", action="store_true")
    parser.add_argument("--output", help="JSON report, updated after every completed case")
    parser.add_argument("--minimum-recall", type=float, default=0.8)
    args = parser.parse_args()
    raise SystemExit(0 if asyncio.run(evaluate(args), loop_factory=loop_factory) else 1)


if __name__ == "__main__":
    main()
