from __future__ import annotations

import uuid

import pytest

from inspector_workers.contracts import validate
from inspector_workers.extract.worker import ExtractWorker
from inspector_workers.parse.worker import ParseWorker

PD = ["Технико-экономические показатели", "Площадь застройки — 1 250,5 м²", "Общая площадь здания: 8 400 м²",
      "Количество этажей: 9", "Бетон класса В35", "Степень огнестойкости здания I"]
RD = ["Общие данные", "Площадь застройки — 1 180 м²", "Общая площадь здания: 8 400 м²",
      "Количество этажей: 9", "Бетон класса В30", "Степень огнестойкости здания II", "Ширина двери 0,8 м"]


def _env(data: dict, event_type: str = "doc.extract.requested") -> dict:
    return {"event_id": str(uuid.uuid4()), "event_type": event_type, "schema_version": 1,
            "occurred_at": "2026-09-28T10:00:00Z", "correlation_id": "p", "object_id": str(uuid.uuid4()),
            "process_id": str(uuid.uuid4()), "data": data}


def parse(settings, storage, pdf_path, stage: str) -> tuple[str, str]:
    """Гоним настоящий parse-воркер, чтобы extract читал реальные layout'ы, а не написанные руками."""
    job, file_id = str(uuid.uuid4()), str(uuid.uuid4())
    storage.files[f"docs/{file_id}.pdf"] = pdf_path.read_bytes()
    penv = _env({"job_id": job, "file_id": file_id, "doc_stage": stage, "sha256": uuid.uuid4().hex * 2,
                 "storage_key": f"docs/{file_id}.pdf", "page_from": 1, "page_to": 20}, "doc.parse.requested")
    out = ParseWorker(settings, storage).process(penv)[0]
    return file_id, out.data["layout_key"]


def test_end_to_end_parse_then_extract(settings, storage, make_pdf):
    pd_file, pd_layout = parse(settings, storage, make_pdf([PD]), "PD")
    rd_file, rd_layout = parse(settings, storage, make_pdf([RD]), "RD")
    job = str(uuid.uuid4())
    env = _env({"job_id": job, "files": [
        {"file_id": pd_file, "stage": "PD", "discipline": "AR", "layout_keys": [pd_layout]},
        {"file_id": rd_file, "stage": "RD", "layout_keys": [rd_layout]}]})
    validate(settings.contracts_dir, "doc.extract.requested", env)

    outs = ExtractWorker(settings, storage).process(env)
    assert len(outs) == 1 and outs[0].topic == "doc.extract.completed"
    data = outs[0].data
    validate(settings.contracts_dir, "doc.extract.completed", {**env, "event_type": "doc.extract.completed", "data": data})
    assert data["facts_key"] == f"facts/{job}.jsonl" and data["model_version"] == "regex"

    facts = storage.jsonl[data["facts_key"]]
    for f in facts:
        validate(settings.contracts_dir, "fact", f)  # каждый факт — по JSON Schema
        assert f["method"] == "vector_text" and 0 <= f["bbox"][0] < f["bbox"][2] <= 1
    by = {(f["param_code"], f["stage"]): f for f in facts}

    assert by[("M-001", "PD")]["value_norm"]["value"] == 1250.5 and by[("M-001", "RD")]["value_norm"]["value"] == 1180.0
    assert by[("M-055", "PD")]["value_norm"]["value"] == "B35" and by[("M-055", "RD")]["value_norm"]["value"] == "B30"
    assert by[("M-022", "PD")]["value_norm"]["value"] == "I" and by[("M-022", "RD")]["value_norm"]["value"] == "II"
    assert by[("M-041", "RD")]["value_norm"] == {"kind": "number", "value": 0.8, "unit_si": "м"}
    assert ("M-041", "PD") not in by, "в ПД такого значения нет — факт не выдумывается"
    assert by[("M-001", "PD")]["file_id"] == pd_file and by[("M-001", "PD")]["file_sha256"]
    assert by[("M-001", "PD")]["page"] == 1
    assert by[("M-001", "PD")]["quote"] == "Площадь застройки — 1 250,5 м²"


def test_fact_ids_are_deterministic(settings, storage, make_pdf):
    file_id, layout = parse(settings, storage, make_pdf([PD]), "PD")
    env = _env({"job_id": str(uuid.uuid4()), "files": [{"file_id": file_id, "stage": "PD", "layout_keys": [layout]}]})
    w = ExtractWorker(settings, storage)
    a = {f["fact_id"] for f in storage.jsonl[w.process(env)[0].data["facts_key"]]}
    b = {f["fact_id"] for f in storage.jsonl[w.process(env)[0].data["facts_key"]]}
    assert a == b and len(a) >= 5, "повторная обработка даёт те же fact_id (идемпотентность)"


def test_params_filter_limits_facts(settings, storage, make_pdf):
    file_id, layout = parse(settings, storage, make_pdf([PD]), "PD")
    env = _env({"job_id": str(uuid.uuid4()), "params": ["M-001"],
                "files": [{"file_id": file_id, "stage": "PD", "layout_keys": [layout]}]})
    facts = storage.jsonl[ExtractWorker(settings, storage).process(env)[0].data["facts_key"]]
    assert {f["param_code"] for f in facts} == {"M-001"}


def test_file_without_stage_gives_no_facts_and_empty_key(settings, storage, make_pdf):
    file_id, layout = parse(settings, storage, make_pdf([PD]), "PD")
    env = _env({"job_id": str(uuid.uuid4()), "files": [{"file_id": file_id, "stage": "", "layout_keys": [layout]}]})
    out = ExtractWorker(settings, storage).process(env)[0]
    assert out.data["facts_key"] == "" and storage.jsonl == {}


def test_dedupe_keeps_most_confident_then_earliest(settings, storage, make_pdf):
    page1 = ["Площадь застройки 100 м²", "Ширина двери 0,9"]      # без единицы -> ниже уверенность
    page2 = ["Площадь застройки 999 м²", "Ширина двери 1,2 м"]
    file_id, layout = parse(settings, storage, make_pdf([page1, page2]), "PD")
    env = _env({"job_id": str(uuid.uuid4()), "files": [{"file_id": file_id, "stage": "PD", "layout_keys": [layout]}]})
    facts = {f["param_code"]: f for f in storage.jsonl[ExtractWorker(settings, storage).process(env)[0].data["facts_key"]]}
    assert facts["M-001"]["value_norm"]["value"] == 100.0, "равная уверенность -> берём раннюю страницу"
    assert facts["M-041"]["value_norm"]["value"] == 1.2, "значение с единицей уверенней значения без единицы"


def test_docx_facts_get_whole_page_bbox(settings, storage, tmp_path):
    from docx import Document
    d = Document(); d.add_paragraph("Площадь застройки — 500 м²")
    p = tmp_path / "a.docx"; d.save(p)
    file_id, job = str(uuid.uuid4()), str(uuid.uuid4())
    storage.files["docs/a.docx"] = p.read_bytes()
    lay = ParseWorker(settings, storage).process(_env({"job_id": job, "file_id": file_id, "doc_stage": "PD", "storage_key": "docs/a.docx"}, "doc.parse.requested"))[0].data["layout_key"]
    env = _env({"job_id": str(uuid.uuid4()), "files": [{"file_id": file_id, "stage": "PD", "layout_keys": [lay]}]})
    fact = storage.jsonl[ExtractWorker(settings, storage).process(env)[0].data["facts_key"]][0]
    assert fact["bbox"] == [0.0, 0.0, 1.0, 1.0] and fact["page"] == 1


def test_failure_event_carries_error_and_is_valid(settings, storage):
    env = _env({"job_id": str(uuid.uuid4()), "files": []})
    out = ExtractWorker(settings, storage).on_failure(env, RuntimeError("llm down"))[0]
    assert out.data["facts_key"] == "" and out.data["error"] == "llm down"
    validate(settings.contracts_dir, "doc.extract.completed", {**env, "event_type": "doc.extract.completed", "data": out.data})
