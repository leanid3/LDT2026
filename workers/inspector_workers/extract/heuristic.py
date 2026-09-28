"""Метод `regex`: применяет PATTERNS к строкам layout. Не угадывает — только явные формулировки."""

from __future__ import annotations

import logging

from . import normalize
from .candidate import Candidate, union_bbox
from .catalog import Param
from .patterns import PATTERNS, Pattern

log = logging.getLogger("workers.extract.regex")


def _build(pat: Pattern, m, param: Param, page: int, quote: str, bbox) -> Candidate | None:
    try:
        if pat.kind == "number":
            raw_num, unit = m.group("num"), (m.group("unit") or "").strip()
            value_norm = normalize.number_norm(raw_num, unit, param.unit)
            raw = f"{raw_num} {unit}".strip()
            unit_out = value_norm["unit_si"]
        elif pat.kind == "ordinal":
            raw = m.group("val").strip()
            value_norm, unit_out = normalize.ordinal_norm(raw, param.scale), None
        else:
            raw = m.group("val").strip()
            value_norm, unit_out = normalize.string_norm(raw), None
    except (ValueError, IndexError):
        return None
    conf = pat.confidence
    if pat.kind == "number" and not (m.group("unit") or "").strip() and param.unit not in ("", "—", "ед.", "шт."):
        conf -= 0.1  # единица не указана рядом со значением — менее надёжно
    return Candidate(pat.code, raw, value_norm, unit_out, page, quote, bbox, round(conf, 2), "regex")


def extract_from_pages(pages: list[dict], catalog: dict[str, Param], only: set[str] | None = None) -> list[Candidate]:
    """pages — страницы из layout JSON: [{"page": n, "lines": [{"text", "bbox"}]}]."""
    out: list[Candidate] = []
    patterns = [p for p in PATTERNS if p.code in catalog and (only is None or p.code in only)]
    for page in pages:
        lines = page["lines"]
        for i, line in enumerate(lines):
            text = line["text"]
            nxt = lines[i + 1] if i + 1 < len(lines) else None
            for pat in patterns:
                if pat.exclude and pat.exclude.search(text):
                    continue
                m = pat.regex.search(text)
                quote, bbox, spanning = text, line["bbox"], False
                if m is None and nxt is not None:
                    # Подпись и значение могут быть на соседних строках (перенос в ячейке таблицы).
                    joined = f"{text} {nxt['text']}"
                    m2 = pat.regex.search(joined)
                    if m2 is not None and m2.start() < len(text):  # совпадение начинается на этой строке
                        m, quote, bbox = m2, joined, union_bbox(line["bbox"], nxt["bbox"])
                        spanning = True
                if m is None:
                    continue
                cand = _build(pat, m, catalog[pat.code], page["page"], quote, bbox)
                if cand:
                    if spanning:
                        # Значение на соседней строке — доказательство шире и менее точное: при равных
                        # условиях побеждает совпадение в одной строке.
                        cand.confidence = round(cand.confidence - 0.05, 2)
                    out.append(cand)
    return out
