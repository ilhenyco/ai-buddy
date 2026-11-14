"""Durable ingestion worker. Restart-safe, single publisher, three attempts per job."""

import argparse
import asyncio
import logging

import httpx

from .config import Settings
from .models import Document
from .provider import DeterministicProvider, OllamaProvider
from .runtime import loop_factory
from .store import Store

log = logging.getLogger(__name__)


async def process_one(store, provider):
    # Session lock survives commits; another worker returns without taking the same job.
    async with await store.connect() as lock:
        row = await (
            await lock.execute("SELECT pg_try_advisory_lock(8213644) AS acquired")
        ).fetchone()
        if not row["acquired"]:
            return False
        try:
            async with await store.connect() as conn:
                await conn.execute("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ")
                job = await (
                    await conn.execute(
                        "SELECT * FROM rag.ingestion_job WHERE status <> 'completed' "
                        "ORDER BY created_at,id LIMIT 1",
                    )
                ).fetchone()
                # A terminal failure blocks subsequent changes until an operator resolves it.
                if not job or job["attempts"] >= 3:
                    return False
                await conn.execute(
                    "UPDATE rag.ingestion_job SET attempts=attempts+1, status='processing' "
                    "WHERE id=%s",
                    (job["id"],),
                )
                rows = await (
                    await conn.execute("SELECT payload FROM rag.corpus_document")
                ).fetchall()
                active = await (await conn.execute("SELECT snapshot_id FROM rag.active")).fetchone()
            documents = {
                row["payload"]["documentId"]: Document.model_validate(row["payload"])
                for row in rows
            }
            if job["payload"] is None:
                documents.pop(job["document_id"], None)
            else:
                documents[job["document_id"]] = Document.model_validate(job["payload"])
            try:
                await store.publish(
                    list(documents.values()),
                    provider,
                    job_id=job["id"],
                    base_snapshot=active["snapshot_id"] if active else None,
                )
            except Exception:
                # No document text, credential or provider exception in logs/status.
                log.warning("Ingestion failed: %s", job["id"])
                async with await store.connect() as conn:
                    await conn.execute(
                        "UPDATE rag.ingestion_job SET status='failed',"
                        "error_code='INGESTION_FAILED' "
                        "WHERE id=%s",
                        (job["id"],),
                    )
            return True
        finally:
            await lock.execute("SELECT pg_advisory_unlock(8213644)")


async def run(once=False):
    settings = Settings()
    store = Store(settings.database_url)
    await store.migrate()
    async with httpx.AsyncClient(timeout=15) as client:
        provider = (
            DeterministicProvider()
            if settings.provider == "deterministic"
            else OllamaProvider(settings, client)
        )
        while True:
            await process_one(store, provider)
            if once:
                return
            await asyncio.sleep(5)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--once", action="store_true")
    args = parser.parse_args()
    logging.basicConfig(level=logging.INFO)
    asyncio.run(run(args.once), loop_factory=loop_factory)


if __name__ == "__main__":
    main()
