"""extract-воркер: doc.extract.requested -> факты JSONL в MinIO -> doc.extract.completed.

Читает layout'ы, которые parse-воркер положил в MinIO, извлекает значения параметров Матрицы
(regex-шаблоны + опционально LLM), приводит к единицам Матрицы, отбрасывает дубли и пишет
contracts/facts.schema.json-совместимый JSONL."""

from __future__ import annotations

import logging
import uuid

from .. import __version__
from ..bus import BaseWorker, OutEvent
from ..config import Settings
from ..contracts import ContractError, validate
from ..storage import Storage
from . import heuristic
from .candidate import Candidate
from .catalog import load_catalog
from .llm import LLMExtractor

log = logging.getLogger("workers.extract")

_FACT_NS = uuid.UUID("6f1f4c1e-0c55-4c5e-9a55-0d1a5e5a1c11")
_STAGES = {"PD", "RD", "ID"}
_WHOLE_PAGE = [0.0, 0.0, 1.0, 1.0]  # нет геометрии (DOCX/XML) — «весь лист»
# Как получен текст (contracts: method = vector_text | ocr | llm | vlm): для regex — способ чтения страницы.
_TEXT_METHOD = {"vector_text": "vector_text", "ocr": "ocr", "docx": "vector_text", "xml": "vector_text"}
EXTRACTOR_VERSION = f"inspector-extract@{__version__}"


class ExtractWorker(BaseWorker):
    name = "extract-worker"
    topic = "doc.extract.requested"

    def __init__(self, settings: Settings, storage: Storage, llm: LLMExtractor | None = None) -> None:
        self.s = settings
        self.storage = storage
        self.catalog = load_catalog(settings.contracts_dir)
        self.llm = llm if llm is not None else (LLMExtractor(settings, self.catalog) if settings.llm_enabled else None)

    def process(self, env: dict) -> list[OutEvent]:
        d = env["data"]
        only = set(d.get("params") or []) or None
        facts: list[dict] = []
        for ref in d["files"]:
            facts.extend(self._facts_for_file(env, d["job_id"], ref, only))

        facts = self._dedupe(facts)
        key = ""
        if facts:
            key = f"facts/{d['job_id']}.jsonl"  # детерминированный ключ — повторная обработка идемпотентна
            self.storage.put_jsonl(key, facts)
        model = "regex" + (f"+{self.s.llm_model}" if self.llm else "")
        log.info("extracted %d facts for job %s (%s)", len(facts), d["job_id"], model)
        return [OutEvent("doc.extract.completed", "doc.extract.completed",
                         {"job_id": d["job_id"], "facts_key": key, "suspicions_key": None, "model_version": model})]

    def on_failure(self, env: dict, error: Exception) -> list[OutEvent]:
        # Оркестратор помечает extract_job FAILED и продолжает (процесс не виснет) — см. engine Error.
        return [OutEvent("doc.extract.completed", "doc.extract.completed",
                         {"job_id": env["data"]["job_id"], "facts_key": "", "suspicions_key": None,
                          "model_version": None, "error": str(error)[:500]})]

    # ---- по файлу ----
    def _facts_for_file(self, env: dict, job_id: str, ref: dict, only: set[str] | None) -> list[dict]:
        stage = ref.get("stage")
        if stage not in _STAGES:
            log.warning("file %s has no stage (%r): facts skipped, upload registry first", ref["file_id"], stage)
            return []

        pages: list[dict] = []
        sha: str | None = None
        for key in ref.get("layout_keys") or []:
            layout = self.storage.get_json(key)
            if layout.get("quality") == "ABSTAIN":
                continue
            sha = sha or layout.get("sha256")
            pages.extend(layout["pages"])
        if not pages:
            return []
        page_method = {p["page"]: _TEXT_METHOD.get(p.get("method", ""), "vector_text") for p in pages}

        cands: list[Candidate] = heuristic.extract_from_pages(pages, self.catalog, only)
        if self.llm is not None:
            cands += self.llm.extract(pages, only)

        facts = []
        for c in cands:
            fact = {
                "fact_id": str(uuid.uuid5(_FACT_NS, f"{job_id}|{ref['file_id']}|{c.param_code}|{c.page}|{c.quote}")),
                "process_id": env.get("process_id"), "file_id": ref["file_id"], "file_sha256": sha,
                "stage": stage, "param_code": c.param_code, "rule_code": None, "element_key": None,
                "value_raw": c.value_raw, "value_norm": c.value_norm, "unit": c.unit, "page": c.page,
                "bbox": c.bbox or _WHOLE_PAGE, "quote": c.quote, "confidence": c.confidence,
                "method": "llm" if c.method == "llm" else page_method.get(c.page, "vector_text"),
                "quality": "OK", "has_change_notice": False, "extractor_version": EXTRACTOR_VERSION,
            }
            if self.s.validate_events:
                try:
                    validate(self.s.contracts_dir, "fact", fact)
                except ContractError as e:  # признак бага в шаблоне/нормализации, а не данных — не роняем задание
                    log.error("own fact violates contract, dropped: %s", e)
                    continue
            facts.append(fact)
        return facts

    @staticmethod
    def _dedupe(facts: list[dict]) -> list[dict]:
        """Один факт на (param_code, stage, element_key): движок берёт первый по стадии, поэтому оставляем
        самый уверенный (при равенстве — самый ранний по странице). Порядок вывода детерминирован."""
        best: dict[tuple, dict] = {}
        for f in facts:
            k = (f["param_code"], f["stage"], f["element_key"])
            cur = best.get(k)
            if cur is None or (f["confidence"], -f["page"]) > (cur["confidence"], -cur["page"]):
                best[k] = f
        return sorted(best.values(), key=lambda f: (f["param_code"], f["stage"], f["page"]))
