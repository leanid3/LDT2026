from __future__ import annotations

import json
import threading
from dataclasses import replace
from http.server import BaseHTTPRequestHandler, HTTPServer

import pytest

from inspector_workers.extract.catalog import load_catalog
from inspector_workers.extract.llm import LLMExtractor
from tests.conftest import CONTRACTS


class _Server:
    """Настоящий HTTP-сервер вместо мока urllib: проверяем реальный путь запроса/ответа."""

    def __init__(self, replies):
        self.replies, self.requests = list(replies), []
        outer = self

        class H(BaseHTTPRequestHandler):
            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                outer.requests.append({"path": self.path, "auth": self.headers.get("Authorization"), "body": body})
                reply = outer.replies.pop(0) if outer.replies else outer.replies_default
                if reply == "500":
                    self.send_response(500); self.end_headers(); return
                data = json.dumps({"choices": [{"message": {"content": reply}}]}).encode()
                self.send_response(200); self.send_header("Content-Type", "application/json"); self.end_headers()
                self.wfile.write(data)

            def log_message(self, *a):
                pass

        self.replies_default = '{"facts": []}'
        self.srv = HTTPServer(("127.0.0.1", 0), H)
        threading.Thread(target=self.srv.serve_forever, daemon=True).start()

    @property
    def url(self):
        return f"http://127.0.0.1:{self.srv.server_port}/v1"

    def close(self):
        self.srv.shutdown()


@pytest.fixture
def catalog():
    return load_catalog(CONTRACTS)


PAGES = [{"page": 3, "lines": [
    {"text": "Раздел ПЗУ", "bbox": [0.1, 0.1, 0.5, 0.12]},
    {"text": "Ширина проезда для пожарной техники 3,5 м", "bbox": [0.1, 0.2, 0.9, 0.22]},
    {"text": "Бетон фундаментов класса B35", "bbox": [0.1, 0.3, 0.7, 0.32]},
]}]


def _extractor(settings, catalog, srv):
    return LLMExtractor(replace(settings, llm_base_url=srv.url, llm_model="test-model", llm_api_key="k"), catalog)


def test_valid_facts_are_kept_with_evidence_from_source_line(settings, catalog):
    srv = _Server([json.dumps({"facts": [
        {"param_code": "M-030", "value": "3,5", "unit": "м", "ref": "3:1"},
        {"param_code": "M-055", "value": "B35", "unit": "", "ref": "3:2"},
    ]})])
    try:
        got = {c.param_code: c for c in _extractor(settings, catalog, srv).extract(PAGES)}
    finally:
        srv.close()
    assert got["M-030"].value_norm == {"kind": "number", "value": 3.5, "unit_si": "м"}
    assert got["M-030"].quote == "Ширина проезда для пожарной техники 3,5 м"
    assert got["M-030"].bbox == [0.1, 0.2, 0.9, 0.22] and got["M-030"].page == 3 and got["M-030"].method == "llm"
    assert got["M-055"].value_norm["value"] == "B35"
    req = srv.requests[0]
    assert req["path"] == "/v1/chat/completions" and req["auth"] == "Bearer k"
    assert req["body"]["model"] == "test-model" and req["body"]["temperature"] == 0
    assert "M-030 | Ширина внутриплощадочных дорог и проездов | м" in req["body"]["messages"][1]["content"]
    assert "[3:1] Ширина проезда" in req["body"]["messages"][1]["content"]


def test_hallucinations_are_dropped(settings, catalog):
    srv = _Server([json.dumps({"facts": [
        {"param_code": "M-030", "value": "4,2", "unit": "м", "ref": "3:1"},    # такого числа в строке нет
        {"param_code": "M-999", "value": "3,5", "unit": "м", "ref": "3:1"},    # нет такого параметра
        {"param_code": "M-008", "value": "28", "unit": "м", "ref": "9:9"},     # нет ref и значения нигде
        {"param_code": "M-030", "value": "не число", "unit": "м", "ref": "3:1"},
        "мусор", {"param_code": "M-030"},
    ]})])
    try:
        got = _extractor(settings, catalog, srv).extract(PAGES)
    finally:
        srv.close()
    assert got == []


def test_wrong_ref_is_relocated_by_value(settings, catalog):
    srv = _Server([json.dumps({"facts": [{"param_code": "M-030", "value": "3,5", "unit": "м", "ref": "3:0"}]})])
    try:
        got = _extractor(settings, catalog, srv).extract(PAGES)
    finally:
        srv.close()
    assert len(got) == 1 and got[0].quote.startswith("Ширина проезда"), "ref указывал на другую строку — значение найдено по тексту"


def test_json_in_code_fence_and_unit_conversion(settings, catalog):
    srv = _Server(['```json\n{"facts": [{"param_code": "M-030", "value": "3500", "unit": "мм", "ref": "3:1"}]}\n```'])
    srv.replies[0] = srv.replies[0].replace("3500", "3,5").replace('"мм"', '"м"')
    try:
        got = _extractor(settings, catalog, srv).extract(PAGES)
    finally:
        srv.close()
    assert [c.param_code for c in got] == ["M-030"]


def test_only_filter_limits_params_in_prompt_and_result(settings, catalog):
    srv = _Server([json.dumps({"facts": [{"param_code": "M-030", "value": "3,5", "unit": "м", "ref": "3:1"},
                                         {"param_code": "M-055", "value": "B35", "unit": "", "ref": "3:2"}]})])
    try:
        got = _extractor(settings, catalog, srv).extract(PAGES, only={"M-055"})
    finally:
        srv.close()
    assert [c.param_code for c in got] == ["M-055"]
    prompt = srv.requests[0]["body"]["messages"][1]["content"]
    assert "M-055 |" in prompt and "M-030 |" not in prompt


def test_server_error_is_tolerated_and_counted(settings, catalog):
    srv = _Server(["500"])
    ex = _extractor(settings, catalog, srv)
    try:
        assert ex.extract(PAGES) == []
    finally:
        srv.close()
    assert ex.failures == 1


def test_large_documents_are_split_into_chunks(settings, catalog):
    pages = [{"page": n, "lines": [{"text": "строка текста " * 20, "bbox": [0, 0, 1, 1]} for _ in range(20)]} for n in range(1, 6)]
    srv = _Server([])
    try:
        _extractor(settings, catalog, srv).extract(pages)
    finally:
        srv.close()
    assert len(srv.requests) >= 3
