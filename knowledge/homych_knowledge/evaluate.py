"""Evaluate retrieval separately from generation; never index this question set."""

import argparse
import asyncio
import hashlib
import json
import time
from collections import Counter
from pathlib import Path

import httpx

from .config import Settings
from .provider import DeterministicProvider, OllamaProvider
from .service import GoIdentity, KnowledgeService
from .store import Store


async def evaluate(args):
    settings = Settings()
    cases = json.loads(await asyncio.to_thread(Path(args.cases).read_text, encoding="utf-8"))
    cases = [case for case in cases if case["split"] == args.split]
    if not cases:
        raise ValueError("empty evaluation split")
    async with httpx.AsyncClient(timeout=15) as client:
        provider = (
            DeterministicProvider()
            if settings.provider == "deterministic"
            else OllamaProvider(settings, client)
        )
        store = Store(settings.database_url)
        service = KnowledgeService(settings, store, provider, GoIdentity(settings, client))
        rows = []
        for case in cases:
            start = time.monotonic()
            result = await service.search(args.subject, case["query"])
            expected = "avito-" + hashlib.sha256(case["path"].encode()).hexdigest()[:24]
            rows.append(
                {
                    "id": case["id"],
                    "hit": any(h.documentId == expected for h in result.items),
                    "seconds": round(time.monotonic() - start, 3),
                    "mode": result.retrievalMode,
                    "snapshotId": result.snapshotId,
                }
            )
        report = {
            "split": args.split,
            "count": len(rows),
            "recallAt5": sum(row["hit"] for row in rows) / len(rows),
            "modes": dict(Counter(row["mode"] for row in rows)),
            "embeddingDigest": await store.active_digest(),
            "results": rows,
        }
        print(json.dumps(report, ensure_ascii=False, indent=2))
        return report["recallAt5"] >= args.minimum_recall


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--cases", default="data/eval.json")
    parser.add_argument("--split", choices=["dev", "test"], default="test")
    parser.add_argument("--subject", default="u-sasha")
    parser.add_argument("--minimum-recall", type=float, default=0.8)
    args = parser.parse_args()
    raise SystemExit(0 if asyncio.run(evaluate(args)) else 1)


if __name__ == "__main__":
    main()
