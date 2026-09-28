"""Нормализация значений (value_norm по contracts/facts.schema.json).

Главный договор с движком правил: value_norm.value выражено в единицах Матрицы (params.unit) —
именно его сравнивает backend (Fact.Comparable). Поэтому «800 мм» для параметра в метрах -> 0.8.
"""

from __future__ import annotations

import re

_CYR_TO_LAT = str.maketrans("АВСЕКМНОРТХ", "ABCEKMHOPTX")

_UNIT_ALIASES = {
    "м2": "м²", "м^2": "м²", "кв.м": "м²", "квм": "м²",
    "м3": "м³", "м^3": "м³", "куб.м": "м³", "кубм": "м³",
}
_LENGTH_M = {"мм": 0.001, "см": 0.01, "м": 1.0}

_NUM_RE = re.compile(r"[-+]?\d+(?:[.,]\d+)?")


def canon_unit(unit: str) -> str:
    u = unit.strip().lower().replace("\u00a0", " ").rstrip(".").strip()
    compact = u.replace(" ", "")
    return _UNIT_ALIASES.get(u) or _UNIT_ALIASES.get(compact) or compact


def parse_number(text: str) -> float:
    """'1 250,5' -> 1250.5. Пробелы/nbsp — разделители тысяч, запятая — десятичная."""
    compact = re.sub(r"[\s  ]", "", text)
    m = _NUM_RE.search(compact)
    if not m:
        raise ValueError(f"нет числа в {text!r}")
    return float(m.group().replace(",", "."))


def to_matrix_unit(value: float, found_unit: str, matrix_unit: str) -> tuple[float, str]:
    """Переводит длины между мм/см/м; для остальных единиц значение не трогает.

    Возвращает (значение, единица результата). Единица результата — как написана в Матрице
    (params.unit), если она задана: движок и человек видят одно и то же обозначение."""
    found, target = canon_unit(found_unit), canon_unit(matrix_unit)
    if found in _LENGTH_M and target in _LENGTH_M and found != target:
        return round(value * _LENGTH_M[found] / _LENGTH_M[target], 9), matrix_unit.strip()
    return value, matrix_unit.strip() or found_unit.strip()


def number_norm(raw_number: str, found_unit: str, matrix_unit: str) -> dict:
    value = parse_number(raw_number)
    value, unit_si = to_matrix_unit(value, found_unit or matrix_unit, matrix_unit)
    return {"kind": "number", "value": value, "unit_si": unit_si}


def ordinal_canon(raw: str) -> str:
    """'В30 ' -> 'B30', 'А500с' -> 'A500C': регистр и кириллица-двойники латиницы (как в rules.normalizeToken)."""
    s = re.sub(r"\s+", "", raw).upper().translate(_CYR_TO_LAT)
    return s.replace(",", ".")


def ordinal_norm(raw: str, scale: str | None) -> dict:
    out = {"kind": "ordinal", "value": ordinal_canon(raw)}
    if scale:
        out["scale"] = scale
    return out


def string_norm(raw: str) -> dict:
    return {"kind": "string", "value": " ".join(raw.split())}
