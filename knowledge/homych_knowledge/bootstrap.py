"""First start of a ready-made stand (docker-compose.release-ai.yml): index the bundled
Avito Playbook once, when no snapshot is active yet. A restart with an index does nothing.

The Playbook clone is baked into the image at RAG_PLAYBOOK_REPO (pinned commit from
data/playbook.lock.json), so GitHub is not needed at run time. Embeddings come from Ollama;
while the models are still downloading the ingestion is retried.
"""

import argparse
import asyncio
import os
import sys
import time

from .config import Settings
from .ingest import run as ingest
from .runtime import loop_factory
from .store import Store


async def bootstrap(store, ingest_once, *, attempts=60, delay=10.0, sleep=asyncio.sleep, log=print):
    await store.migrate()
    if await store.active_digest():
        log("knowledge index is ready, nothing to ingest")
        return True
    for attempt in range(1, attempts + 1):
        try:
            await ingest_once()
            log("Avito Playbook indexed")
            return True
        except Exception as exc:  # the models may still be downloading
            log(f"ingestion attempt {attempt}/{attempts} failed: {exc}")
            if attempt == attempts:
                return False
            await sleep(delay)
    return False


async def main_async():
    settings = Settings()
    store = Store(settings.database_url)
    repo = os.environ.get("RAG_PLAYBOOK_REPO", "/opt/playbook")
    args = argparse.Namespace(
        command="playbook", repo=repo, lock="data/playbook.lock.json", fixtures=None
    )
    started = time.monotonic()
    ok = await bootstrap(store, lambda: ingest(args))
    print(f"bootstrap finished in {time.monotonic() - started:.0f}s", flush=True)
    return ok


def main():
    ok = asyncio.run(main_async(), loop_factory=loop_factory)
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
