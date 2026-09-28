"""Рантайм воркера против настоящего Postgres (миграции применены). Включается TEST_DATABASE_DSN, например:
TEST_DATABASE_DSN="host=localhost port=5432 user=postgres password=postgres dbname=inspector" make test"""

from __future__ import annotations

import json
import os
import uuid
from dataclasses import replace

import psycopg
import pytest

from inspector_workers.bus import BaseWorker, OutEvent, Runner
from inspector_workers.contracts import validate

DSN = os.environ.get("TEST_DATABASE_DSN")
pytestmark = pytest.mark.skipif(not DSN, reason="нужен TEST_DATABASE_DSN (Postgres с миграциями)")


class StubWorker(BaseWorker):
    name = f"stub-{uuid.uuid4().hex[:8]}"  # своё имя consumer'а — тесты не мешают друг другу и реальным данным
    topic = "doc.parse.requested"

    def __init__(self, fail: bool = False):
        self.fail, self.calls = fail, 0

    def process(self, env):
        self.calls += 1
        if self.fail:
            raise RuntimeError("boom")
        d = env["data"]
        return [OutEvent("doc.parse.completed", "doc.parse.completed",
                         {"job_id": d["job_id"], "file_id": d["file_id"], "quality": "OK", "layout_key": "layouts/x.json"})]

    def on_failure(self, env, error):
        d = env["data"]
        return [OutEvent("doc.parse.completed", "doc.parse.completed",
                         {"job_id": d["job_id"], "file_id": d["file_id"], "quality": "FAILED", "layout_key": None, "error": str(error)})]


def _event(**over) -> dict:
    env = {"event_id": str(uuid.uuid4()), "event_type": "doc.parse.requested", "schema_version": 1,
           "occurred_at": "2026-09-28T10:00:00Z", "correlation_id": "c", "object_id": str(uuid.uuid4()),
           "process_id": str(uuid.uuid4()),
           "data": {"job_id": str(uuid.uuid4()), "file_id": str(uuid.uuid4()), "storage_key": "k/a.pdf"}}
    env.update(over)
    return env


@pytest.fixture
def runner(settings):
    r = Runner(StubWorker(), replace(settings, db_dsn=DSN, max_attempts=2))
    yield r
    r._db().close()


def outbox_for(process_id: str) -> list[dict]:
    with psycopg.connect(DSN) as c, c.cursor() as cur:
        cur.execute("SELECT event_type, topic, msg_key, status, payload FROM outbox_events WHERE aggregate_id = %s ORDER BY created_at",
                    (process_id,))
        return [{"event_type": r[0], "topic": r[1], "msg_key": r[2], "status": r[3], "payload": r[4]} for r in cur.fetchall()]


def test_result_goes_to_outbox_once_with_valid_envelope(runner, settings):
    env = _event()
    runner.handle_message(json.dumps(env).encode(), "doc.parse.requested")
    rows = outbox_for(env["process_id"])
    assert len(rows) == 1
    row = rows[0]
    assert row["topic"] == "doc.parse.completed" and row["status"] == "pending" and row["msg_key"] == env["object_id"]
    validate(settings.contracts_dir, "doc.parse.completed", row["payload"])  # то, что читает relay -> Kafka -> Go engine
    assert row["payload"]["process_id"] == env["process_id"] and row["payload"]["correlation_id"] == "c"

    # тот же event_id ещё раз -> работа не выполняется и в outbox не добавляется
    runner.handle_message(json.dumps(env).encode(), "doc.parse.requested")
    assert runner.worker.calls == 1 and len(outbox_for(env["process_id"])) == 1


def test_concurrent_duplicate_commit_is_rejected(runner):
    env = _event()
    out = [OutEvent("doc.parse.completed", "doc.parse.completed",
                    {"job_id": str(uuid.uuid4()), "file_id": str(uuid.uuid4()), "quality": "OK"})]
    assert runner.commit_result(env, out) is True
    assert runner.commit_result(env, out) is False
    assert len(outbox_for(env["process_id"])) == 1


def test_exhausted_retries_publish_failed_result_and_dlq(settings, monkeypatch):
    monkeypatch.setattr("time.sleep", lambda s: None)
    r = Runner(StubWorker(fail=True), replace(settings, db_dsn=DSN, max_attempts=3))
    env = _event()
    r.handle_message(json.dumps(env).encode(), "doc.parse.requested")
    assert r.worker.calls == 3
    by_topic = {row["topic"]: row for row in outbox_for(env["process_id"])}
    assert by_topic["doc.parse.completed"]["payload"]["data"]["quality"] == "FAILED", "процесс не должен зависнуть"
    dlq = by_topic["doc.parse.requested.dlq"]["payload"]["data"]
    assert dlq["original"]["event_id"] == env["event_id"] and dlq["error"] == "boom"
    r._db().close()


def test_contract_violating_message_goes_straight_to_dlq_without_retries(runner):
    env = _event()
    del env["data"]["storage_key"]  # нарушение контракта doc.parse.requested
    runner.handle_message(json.dumps(env).encode(), "doc.parse.requested")
    assert runner.worker.calls == 0
    rows = outbox_for(env["process_id"])
    assert [r["topic"] for r in rows] == ["doc.parse.requested.dlq"]
    assert "storage_key" in rows[0]["payload"]["data"]["error"]


def test_garbage_is_skipped_without_exception(runner):
    runner.handle_message(b"not json at all", "doc.parse.requested")
    runner.handle_message(b'"just a string"', "doc.parse.requested")
    assert runner.worker.calls == 0


def test_foreign_event_type_is_ignored(runner):
    env = _event(event_type="doc.extract.requested", data={"job_id": str(uuid.uuid4()), "files": []})
    runner.handle_message(json.dumps(env).encode(), "doc.parse.requested")
    assert runner.worker.calls == 0 and outbox_for(env["process_id"]) == []
