import pytest

from homych_knowledge.evaluate import diagnose, summarize
from homych_knowledge.store import developer_career


def row(**changes):
    return {
        "answerable": True,
        "status": "answered",
        "hit": True,
        "expectedInPrompt": True,
        "generated": {"answered": True},
        "citesExpected": True,
        "mode": "hybrid",
        **changes,
    }


@pytest.mark.parametrize(
    "changes,reason",
    [
        ({"hit": False}, "retrieval_miss"),
        ({"expectedInPrompt": False}, "prompt_miss"),
        ({"generated": {"answered": False}, "status": "no_evidence"}, "model_abstention"),
        ({"status": "no_evidence"}, "answer_rejected"),
        ({"status": "no_evidence", "verified": False}, "verification_rejected"),
        ({"citesExpected": False}, "wrong_source"),
        ({"status": "unavailable"}, "unavailable"),
        ({"answerable": False, "status": "no_evidence"}, "correct_abstention"),
        ({"answerable": False}, "false_answer"),
    ],
)
def test_attribution_distinguishes_pipeline_failures(changes, reason):
    assert diagnose(row(**changes)) == reason


def test_negative_cases_do_not_inflate_retrieval_recall():
    rows = [
        row(),
        row(hit=False, status="no_evidence", citesExpected=False),
        row(answerable=False, hit=None, status="no_evidence", citesExpected=False),
        row(answerable=False, hit=None, citesExpected=False),
    ]
    for item in rows:
        item["diagnosis"] = diagnose(item)
    result = summarize(rows, generation=True)
    assert result["recallAt5"] == 0.5
    assert result["positiveStatuses"] == {"answered": 1, "no_evidence": 1}
    assert result["correctAbstentions"] == 1
    assert result["falseAnswers"] == 1


def test_career_role_aliases_do_not_hijack_specialized_or_profiling_queries():
    assert developer_career("Как оценивают уровень разработчика?")
    assert not developer_career("Какие уровни у BI-разработчика?")
    assert not developer_career("Как разработчику профилировать Go-приложение?")
