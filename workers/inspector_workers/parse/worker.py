"""parse-воркер: doc.parse.requested -> layout в MinIO -> doc.parse.completed (backend-plan.md §6.2)."""

from __future__ import annotations

import logging
import zipfile
from pathlib import Path

from ..bus import BaseWorker, OutEvent
from ..config import Settings
from ..storage import Storage
from .docx import parse_docx
from .layout import LAYOUT_VERSION, ParseResult
from .pdf import parse_pdf
from .xmlfile import parse_xml

log = logging.getLogger("workers.parse")


def detect_kind(path: Path) -> str:
    """pdf | docx | xml по содержимому (расширению и content_type не доверяем — файл мог быть переименован)."""
    with path.open("rb") as f:
        head = f.read(8)
    if head.startswith(b"%PDF-"):
        return "pdf"
    if head.startswith(b"PK\x03\x04"):
        try:
            with zipfile.ZipFile(path) as z:
                if "word/document.xml" in z.namelist():
                    return "docx"
        except zipfile.BadZipFile:
            pass
        raise ValueError("zip-архив, но не DOCX")
    if head.lstrip(b"\xef\xbb\xbf").lstrip().startswith(b"<"):
        return "xml"
    raise ValueError("неподдерживаемый формат файла")


class ParseWorker(BaseWorker):
    name = "parse-worker"
    topic = "doc.parse.requested"

    def __init__(self, settings: Settings, storage: Storage) -> None:
        self.s = settings
        self.storage = storage

    def process(self, env: dict) -> list[OutEvent]:
        d = env["data"]
        job_id, file_id = d["job_id"], d["file_id"]
        page_from, page_to = d.get("page_from"), d.get("page_to")

        local = self.storage.download_to_temp(d["storage_key"])
        try:
            kind = detect_kind(local)
            if kind == "pdf":
                result = parse_pdf(local, page_from, page_to, use_ocr=self.s.ocr_enabled)
            elif kind == "docx":
                result = parse_docx(local)
            else:
                result = parse_xml(local)
        finally:
            local.unlink(missing_ok=True)

        layout_key = self._store_layout(env, d, kind, result)
        log.info("parsed %s (%s): quality=%s %s", file_id, kind, result.quality, result.stats)
        return [OutEvent("doc.parse.completed", "doc.parse.completed", {
            "job_id": job_id, "file_id": file_id, "quality": result.quality, "layout_key": layout_key,
            "page_from": page_from, "page_to": page_to, "stats": {**result.stats, "kind": kind},
        })]

    def _store_layout(self, env: dict, d: dict, kind: str, result: ParseResult) -> str | None:
        if not result.pages:
            return None
        # Ключ детерминирован по job_id: повторная обработка перезапишет тот же объект (идемпотентно).
        key = f"layouts/{d['job_id']}.json"
        self.storage.put_json(key, {
            "version": LAYOUT_VERSION, "kind": kind, "job_id": d["job_id"], "file_id": d["file_id"],
            "sha256": d.get("sha256"), "stage": d.get("doc_stage"), "discipline": d.get("discipline"),
            "process_id": env.get("process_id"), "quality": result.quality,
            "pages": [p.to_json() for p in result.pages],
        })
        return key

    def on_failure(self, env: dict, error: Exception) -> list[OutEvent]:
        d = env["data"]
        # FAILED: оркестратор помечает job FAILED, процесс не блокируется, файл даёт NOT_COMPARABLE (ТЗ 9.1).
        return [OutEvent("doc.parse.completed", "doc.parse.completed", {
            "job_id": d["job_id"], "file_id": d["file_id"], "quality": "FAILED", "layout_key": None,
            "page_from": d.get("page_from"), "page_to": d.get("page_to"), "error": str(error)[:500],
        })]
