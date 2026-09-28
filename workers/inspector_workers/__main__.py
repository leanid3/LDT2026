"""Запуск: python -m inspector_workers parse|extract  (настройки — из ENV, см. config.py)."""

from __future__ import annotations

import argparse
import json
import logging
import signal
import sys
import threading

from .bus import Runner
from .config import Settings
from .storage import Storage


class _JsonFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        doc = {"timestamp": self.formatTime(record, "%Y-%m-%dT%H:%M:%S%z"), "level": record.levelname,
               "logger": record.name, "msg": record.getMessage()}
        if record.exc_info:
            doc["exc"] = self.formatException(record.exc_info)
        return json.dumps(doc, ensure_ascii=False)


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="inspector_workers")
    ap.add_argument("worker", choices=["parse", "extract"])
    args = ap.parse_args(argv)

    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(_JsonFormatter())
    logging.basicConfig(level=logging.INFO, handlers=[handler])
    log = logging.getLogger("workers")

    settings = Settings.from_env()
    storage = Storage(settings)
    storage.ensure_bucket()

    if args.worker == "parse":
        from .parse.worker import ParseWorker
        worker = ParseWorker(settings, storage)
    else:
        from .extract.worker import ExtractWorker
        worker = ExtractWorker(settings, storage)
        log.info("extract: LLM %s", f"включён ({settings.llm_model})" if settings.llm_enabled else "выключен (только regex)")

    stop = threading.Event()
    for sig in (signal.SIGINT, signal.SIGTERM):
        signal.signal(sig, lambda *_: stop.set())
    Runner(worker, settings).run(stop)
    log.info("worker %s stopped", worker.name)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
