import json
import subprocess

import pytest
from pydantic import ValidationError

from homych_knowledge.config import Settings
from homych_knowledge.ingest import playbook_documents
from homych_knowledge.models import Document


def test_git_ingestion_pinned_commit_excludes_non_markdown(tmp_path):
    subprocess.run(["git", "init", "--quiet", str(tmp_path)], check=True)
    (tmp_path / "readme.md").write_text("# Корпус\nТолько этот текст.", encoding="utf-8")
    (tmp_path / "private.txt").write_text("not indexed", encoding="utf-8")
    subprocess.run(["git", "-C", str(tmp_path), "add", "."], check=True)
    subprocess.run(
        [
            "git",
            "-C",
            str(tmp_path),
            "-c",
            "user.name=Test",
            "-c",
            "user.email=test@example.invalid",
            "commit",
            "--quiet",
            "-m",
            "fixture",
        ],
        check=True,
    )
    commit = subprocess.check_output(
        ["git", "-C", str(tmp_path), "rev-parse", "HEAD"], text=True
    ).strip()
    docs = playbook_documents(tmp_path, commit)
    assert len(docs) == 1 and docs[0].revision == commit
    assert commit in docs[0].url
    with pytest.raises(ValueError):
        playbook_documents(tmp_path, "HEAD")


def test_fixture_metadata_and_evaluation_splits():
    from pathlib import Path

    fixtures = json.loads(Path("data/fixtures.json").read_text(encoding="utf-8"))
    assert all(Document.model_validate(d).synthetic for d in fixtures)
    cases = json.loads(Path("data/eval.json").read_text(encoding="utf-8"))
    assert sum(c["split"] == "dev" for c in cases) == 20
    assert sum(c["split"] == "test" for c in cases) == 20
    assert len({c["query"] for c in cases}) == 40


def test_demo_features_fail_closed():
    with pytest.raises(ValidationError):
        Settings(service_token="test-token-with-at-least-24", provider="deterministic")
    with pytest.raises(ValidationError):
        Settings(service_token="short")
