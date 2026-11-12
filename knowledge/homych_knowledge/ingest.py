import argparse
import asyncio
import json
import subprocess
import tempfile
from datetime import UTC, datetime
from pathlib import Path

import httpx

from .config import Settings
from .models import Document
from .provider import DeterministicProvider, OllamaProvider
from .runtime import loop_factory
from .store import Store


def git(repo: Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repo), *args], check=True, capture_output=True, encoding="utf-8"
    ).stdout


def playbook_documents(repo: Path, commit: str) -> list[Document]:
    # Git object reads do not execute hooks or follow worktree symlinks.
    actual = git(repo, "rev-parse", "--verify", f"{commit}^{{commit}}").strip()
    if actual != commit:
        raise ValueError("revision must be a full commit SHA")
    documents = []
    for line in git(repo, "ls-tree", "-r", commit).splitlines():
        meta, path = line.split("\t", 1)
        if not meta.startswith("100644 blob ") or not path.lower().endswith(".md"):
            continue
        text = git(repo, "show", f"{commit}:{path}")
        title = next((s.lstrip("# ") for s in text.splitlines() if s.startswith("# ")), path)
        import hashlib
        from urllib.parse import quote

        doc_id = "avito-" + hashlib.sha256(path.encode()).hexdigest()[:24]
        documents.append(
            Document(
                documentId=doc_id,
                revision=commit,
                title=title,
                text=text,
                url=f"https://github.com/avito-tech/playbook/blob/{commit}/{quote(path)}",
                validFrom=datetime(2026, 1, 1, tzinfo=UTC),
                expiresAt=datetime(2100, 1, 1, tzinfo=UTC),
            )
        )
    return documents


async def run(args):
    settings = Settings()
    store = Store(settings.database_url)
    await store.migrate()
    if args.command == "revoke":
        await store.revoke(args.document_id)
        print(json.dumps({"revoked": args.document_id}))
        return
    if args.command == "migrate":
        print("Schema ready")
        return
    lock = json.loads(await asyncio.to_thread(Path(args.lock).read_text, encoding="utf-8"))
    with tempfile.TemporaryDirectory(prefix="homych-playbook-") as folder:
        repo = Path(args.repo) if args.repo else Path(folder)
        if not args.repo:
            await asyncio.to_thread(
                subprocess.run,
                [
                    "git",
                    "clone",
                    "--quiet",
                    "--no-checkout",
                    "https://github.com/avito-tech/playbook.git",
                    str(repo),
                ],
                check=True,
            )
        documents = await asyncio.to_thread(playbook_documents, repo, lock["commit"])
    if args.fixtures:
        if not settings.demo_mode:
            raise ValueError("fixtures require RAG_DEMO_MODE=true")
        fixtures = json.loads(
            await asyncio.to_thread(Path(args.fixtures).read_text, encoding="utf-8")
        )
        documents.extend(Document.model_validate(doc) for doc in fixtures)
    async with httpx.AsyncClient(timeout=15) as client:
        provider = (
            DeterministicProvider()
            if settings.provider == "deterministic"
            else OllamaProvider(settings, client)
        )
        snapshot = await store.publish(documents, provider)
    print(
        json.dumps(
            {
                "snapshotId": snapshot,
                "documents": len(documents),
                "commit": lock["commit"],
                "provider": settings.provider,
            }
        )
    )


def main():
    parser = argparse.ArgumentParser(description="Offline corpus publication; operator-only CLI")
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("migrate")
    revoke = commands.add_parser("revoke")
    revoke.add_argument("document_id")
    playbook = commands.add_parser("playbook")
    playbook.add_argument("--repo", help="Existing local Playbook clone; otherwise clone into temp")
    playbook.add_argument("--lock", default="data/playbook.lock.json")
    playbook.add_argument("--fixtures", help="Optional synthetic documents JSON (demo only)")
    args = parser.parse_args()
    asyncio.run(run(args), loop_factory=loop_factory)


if __name__ == "__main__":
    main()
