"""Метод `llm`: извлечение значений параметров Матрицы моделью через OpenAI-совместимый API
(POST {LLM_BASE_URL}/chat/completions — подходит и llama-server/vLLM/Ollama, и облачные провайдеры).

Принцип: модель только УКАЗЫВАЕТ, где значение (ref «страница:строка») и какое оно; доказательством
служит строка исходного текста. Каждый ответ проверяется:
  - код параметра существует в Матрице;
  - значение действительно присутствует в указанной строке (иначе — выдумка, отбрасываем);
  - значение нормализуется по типу параметра.
Факт без верифицируемой цитаты и bbox не годится как доказательство (ТЗ, метрики «Доказательства»).
"""

from __future__ import annotations

import json
import logging
import re
import urllib.error
import urllib.request

from . import normalize
from .candidate import Candidate
from .catalog import Param
from ..config import Settings

log = logging.getLogger("workers.extract.llm")

CHUNK_CHARS = 7000
LLM_CONFIDENCE = 0.7

_SYSTEM = (
    "Ты извлекаешь значения контролируемых параметров из текста строительной документации (ПД/РД/ИД). "
    "Верни ТОЛЬКО JSON вида {\"facts\": [{\"param_code\": \"M-001\", \"value\": \"...\", \"unit\": \"...\", "
    "\"ref\": \"страница:строка\"}]}. Правила: значение копируй из текста дословно, ничего не вычисляй и не "
    "придумывай; ref — метка строки из квадратных скобок, где найдено значение; если параметра в тексте нет — "
    "не включай его; один параметр — не более одного значения на строку."
)


class LLMError(RuntimeError):
    pass


def _squash(s: str) -> str:
    return re.sub(r"[\s ]", "", s).lower().replace(",", ".")


class LLMExtractor:
    def __init__(self, settings: Settings, catalog: dict[str, Param]) -> None:
        self.s = settings
        self.catalog = catalog
        self.failures = 0

    # ---- публичный API ----
    def extract(self, pages: list[dict], only: set[str] | None = None) -> list[Candidate]:
        params = [p for p in self.catalog.values() if only is None or p.code in only]
        out: list[Candidate] = []
        for chunk in self._chunks(pages):
            try:
                raw = self._chat(self._prompt(params, chunk["text"]))
            except LLMError as e:
                self.failures += 1
                log.warning("LLM chunk failed, skipping: %s", e)
                continue
            out.extend(self._validate(raw, chunk, only))
        return out

    # ---- чанки ----
    def _chunks(self, pages: list[dict]) -> list[dict]:
        chunks: list[dict] = []
        cur_lines: list[str] = []
        cur_index: dict[str, dict] = {}
        size = 0
        for page in pages:
            for i, line in enumerate(page["lines"]):
                ref = f"{page['page']}:{i}"
                rendered = f"[{ref}] {line['text']}"
                if size + len(rendered) > CHUNK_CHARS and cur_lines:
                    chunks.append({"text": "\n".join(cur_lines), "index": cur_index})
                    cur_lines, cur_index, size = [], {}, 0
                cur_lines.append(rendered)
                cur_index[ref] = {"page": page["page"], "text": line["text"], "bbox": line["bbox"]}
                size += len(rendered) + 1
        if cur_lines:
            chunks.append({"text": "\n".join(cur_lines), "index": cur_index})
        return chunks

    def _prompt(self, params: list[Param], text: str) -> list[dict]:
        catalog = "\n".join(f"{p.code} | {p.name} | {p.unit}" for p in params)
        user = f"Параметры (код | название | единица):\n{catalog}\n\nТекст документа:\n{text}"
        return [{"role": "system", "content": _SYSTEM}, {"role": "user", "content": user}]

    # ---- HTTP ----
    def _chat(self, messages: list[dict]) -> dict:
        body = json.dumps({"model": self.s.llm_model, "messages": messages, "temperature": 0,
                           "response_format": {"type": "json_object"}}).encode("utf-8")
        headers = {"Content-Type": "application/json"}
        if self.s.llm_api_key:
            headers["Authorization"] = f"Bearer {self.s.llm_api_key}"
        req = urllib.request.Request(f"{self.s.llm_base_url}/chat/completions", data=body, headers=headers, method="POST")
        try:
            with urllib.request.urlopen(req, timeout=self.s.llm_timeout) as resp:
                payload = json.loads(resp.read().decode("utf-8"))
            content = payload["choices"][0]["message"]["content"]
            return json.loads(_strip_fences(content))
        except (urllib.error.URLError, TimeoutError, ValueError, KeyError, IndexError) as e:
            raise LLMError(str(e)) from e

    # ---- проверка ответа ----
    def _validate(self, raw: dict, chunk: dict, only: set[str] | None) -> list[Candidate]:
        out: list[Candidate] = []
        facts = raw.get("facts") if isinstance(raw, dict) else None
        if not isinstance(facts, list):
            return out
        index: dict[str, dict] = chunk["index"]
        for f in facts:
            if not isinstance(f, dict):
                continue
            code, value = str(f.get("param_code", "")), str(f.get("value", "")).strip()
            param = self.catalog.get(code)
            if param is None or not value or (only is not None and code not in only):
                continue
            line = index.get(str(f.get("ref", "")))
            if line is None or _squash(value) not in _squash(line["text"]):
                # ref неверный — ищем значение в других строках чанка; нет нигде — это выдумка.
                line = next((ln for ln in index.values() if _squash(value) in _squash(ln["text"])), None)
            if line is None:
                log.info("dropped unverifiable LLM fact %s=%r", code, value)
                continue
            unit = str(f.get("unit") or "").strip()
            try:
                if param.data_type == "number":
                    norm = normalize.number_norm(value, unit, param.unit)
                    unit_out = norm["unit_si"]
                elif param.data_type == "ordinal":
                    norm, unit_out = normalize.ordinal_norm(value, param.scale), None
                else:
                    norm, unit_out = normalize.string_norm(value), None
            except ValueError:
                continue
            out.append(Candidate(code, f"{value} {unit}".strip(), norm, unit_out, line["page"], line["text"],
                                 line["bbox"], LLM_CONFIDENCE, "llm"))
        return out


def _strip_fences(s: str) -> str:
    s = s.strip()
    m = re.match(r"^```(?:json)?\s*(.*?)\s*```$", s, re.DOTALL)
    return m.group(1) if m else s
