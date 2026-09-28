"""Рантайм воркера: Kafka-consumer с ручным commit + идемпотентность + transactional outbox.

Повторяет гарантии Go-сервисов (backend-plan.md §6.3, backend/CLAUDE.md правила 4-5):
  - enable.auto.commit=false, offset коммитится только после COMMIT транзакции Postgres;
  - дедупликация по consumed_events(consumer_name, event_id);
  - публикация только через INSERT в outbox_events в той же транзакции (в Kafka пишет cmd/relay);
  - до N попыток с backoff, затем воркер сам публикует «сбойный» результат (чтобы процесс не завис)
    и копию исходного события в <topic>.dlq.
Тяжёлая работа (парсинг, LLM) выполняется ДО открытия транзакции: она идемпотентна (детерминированные
ключи в MinIO), а транзакция остаётся короткой.
"""

from __future__ import annotations

import json
import logging
import threading
import time
import uuid
from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any

import psycopg
from confluent_kafka import Consumer, KafkaError, Message, TopicPartition

from .config import Settings
from .contracts import ContractError, validate

log = logging.getLogger("workers.bus")

# События, для которых есть JSON Schema в contracts/events.
_SCHEMA_EVENTS = {"doc.parse.requested", "doc.parse.completed", "doc.extract.requested", "doc.extract.completed"}


@dataclass
class OutEvent:
    event_type: str
    topic: str
    data: dict[str, Any] = field(default_factory=dict)


class BaseWorker(ABC):
    name: str  # consumer_name в consumed_events и group.id
    topic: str  # входной топик

    @abstractmethod
    def process(self, env: dict) -> list[OutEvent]:
        """Обрабатывает событие, возвращает исходящие. Исключение = попытка не удалась."""

    def on_failure(self, env: dict, error: Exception) -> list[OutEvent]:
        """Что публиковать, когда попытки исчерпаны (обычно — «FAILED»-результат). По умолчанию ничего."""
        return []


class Runner:
    def __init__(self, worker: BaseWorker, settings: Settings) -> None:
        self.worker = worker
        self.s = settings
        self._conn: psycopg.Connection | None = None

    # ---- БД ----
    def _db(self) -> psycopg.Connection:
        if self._conn is None or self._conn.closed:
            self._conn = psycopg.connect(self.s.db_dsn)
        return self._conn

    def _already_consumed(self, event_id: str) -> bool:
        with self._db().cursor() as cur:
            cur.execute("SELECT 1 FROM consumed_events WHERE consumer_name = %s AND event_id = %s",
                        (self.worker.name, event_id))
            found = cur.fetchone() is not None
        self._db().rollback()  # закрыть неявную транзакцию SELECT
        return found

    def commit_result(self, env: dict, outs: list[OutEvent]) -> bool:
        """Атомарно: отметка consumed + outbox исходящих. False — событие уже было обработано."""
        conn = self._db()
        try:
            with conn.transaction():
                with conn.cursor() as cur:
                    cur.execute(
                        "INSERT INTO consumed_events (consumer_name, event_id) VALUES (%s, %s) "
                        "ON CONFLICT DO NOTHING RETURNING event_id",
                        (self.worker.name, env["event_id"]),
                    )
                    if cur.fetchone() is None:
                        return False
                    for out in outs:
                        self._outbox_insert(cur, env, out)
            return True
        except psycopg.OperationalError:
            self._conn = None  # переподключимся на следующей попытке
            raise

    def _outbox_insert(self, cur: psycopg.Cursor, src: dict, out: OutEvent) -> None:
        event_id = str(uuid.uuid4())
        envelope = {
            "event_id": event_id,
            "event_type": out.event_type,
            "schema_version": 1,
            "occurred_at": datetime.now(timezone.utc).isoformat(),
            "correlation_id": src.get("correlation_id") or src.get("process_id"),
            "object_id": src.get("object_id"),
            "process_id": src.get("process_id"),
            "data": out.data,
        }
        if self.s.validate_events and out.event_type in _SCHEMA_EVENTS:
            validate(self.s.contracts_dir, out.event_type, envelope)  # свой выход — по тому же контракту
        cur.execute(
            "INSERT INTO outbox_events (event_id, aggregate_id, event_type, topic, msg_key, payload) "
            "VALUES (%s, %s, %s, %s, %s, %s::jsonb)",
            (event_id, src["process_id"], out.event_type, out.topic, src.get("object_id") or "",
             json.dumps(envelope, ensure_ascii=False)),
        )

    # ---- обработка одного сообщения ----
    def handle_message(self, raw: bytes, topic: str) -> None:
        try:
            env = json.loads(raw)
            if not isinstance(env, dict):
                raise ValueError("событие должно быть JSON-объектом")
            if self.s.validate_events and env.get("event_type") in _SCHEMA_EVENTS:
                validate(self.s.contracts_dir, env["event_type"], env)
        except (ValueError, ContractError) as e:
            # Ядовитое сообщение: повтор не поможет. Сохраняем в DLQ и идём дальше.
            log.error("poison message, sending to DLQ: %s", e)
            self._dlq_poison(raw, topic, str(e))
            return

        if env.get("event_type") != self.worker.topic:
            return  # чужой тип в нашем топике — не наше
        if self._already_consumed(env["event_id"]):
            log.info("duplicate event skipped", extra={"event_id": env["event_id"]})
            return

        outs: list[OutEvent]
        last_err: Exception | None = None
        for attempt in range(1, self.s.max_attempts + 1):
            try:
                outs = self.worker.process(env)
                last_err = None
                break
            except Exception as e:  # noqa: BLE001 — воркер не должен падать из-за одного задания
                last_err = e
                log.warning("attempt %d/%d failed: %s", attempt, self.s.max_attempts, e, exc_info=True)
                if attempt < self.s.max_attempts:
                    time.sleep(min(2 ** (attempt - 1), 30))
        if last_err is not None:
            log.error("giving up on event %s: %s", env["event_id"], last_err)
            outs = list(self.worker.on_failure(env, last_err))
            outs.append(OutEvent(env["event_type"] + ".dlq", topic + ".dlq",
                                 {"original": env, "error": str(last_err)}))

        self.commit_result(env, outs)

    def _dlq_poison(self, raw: bytes, topic: str, error: str) -> None:
        try:
            env = json.loads(raw)
        except ValueError:
            return  # даже не JSON — нечего ни адресовать, ни сохранять (offset коммитим, сообщение пропускаем)
        if not isinstance(env, dict) or "process_id" not in env or "event_id" not in env:
            return
        self.commit_result(env, [OutEvent(str(env.get("event_type")) + ".dlq", topic + ".dlq",
                                          {"original": env, "error": error})])

    # ---- цикл ----
    def run(self, stop: threading.Event) -> None:
        consumer = Consumer({
            "bootstrap.servers": self.s.kafka_brokers,
            "group.id": f"worker-{self.worker.name}",
            "enable.auto.commit": False,
            "auto.offset.reset": "earliest",
            "isolation.level": "read_committed",
            "max.poll.interval.ms": 1_800_000,  # LLM/парсинг длинные, как у Go-consumer'ов (§6.3)
            "session.timeout.ms": 30_000,
        })
        consumer.subscribe([self.worker.topic])
        log.info("worker %s consuming %s", self.worker.name, self.worker.topic)
        try:
            while not stop.is_set():
                msg: Message | None = consumer.poll(1.0)
                if msg is None:
                    continue
                if msg.error():
                    if msg.error().code() != KafkaError._PARTITION_EOF:
                        log.error("kafka error: %s", msg.error())
                    continue
                try:
                    self.handle_message(msg.value(), msg.topic())
                except Exception:  # noqa: BLE001
                    # Не смогли зафиксировать результат (БД недоступна и т.п.): offset НЕ коммитим,
                    # сообщение будет доставлено снова (идемпотентно благодаря consumed_events).
                    log.exception("failed to commit result, rewinding to redeliver")
                    time.sleep(2)
                    # Без seek следующий успешный commit «перепрыгнул» бы это сообщение.
                    consumer.seek(TopicPartition(msg.topic(), msg.partition(), msg.offset()))
                    continue
                consumer.commit(msg, asynchronous=False)
        finally:
            consumer.close()
            if self._conn is not None:
                self._conn.close()
