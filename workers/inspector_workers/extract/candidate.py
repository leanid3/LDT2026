from __future__ import annotations

from dataclasses import dataclass, field


@dataclass
class Candidate:
    """Найденное значение параметра до превращения в Fact (contracts/facts.schema.json)."""

    param_code: str
    value_raw: str
    value_norm: dict | None
    unit: str | None
    page: int
    quote: str
    bbox: list[float] | None  # None — у источника нет геометрии
    confidence: float
    method: str  # regex | llm
    extra: dict = field(default_factory=dict)


def union_bbox(a: list[float] | None, b: list[float] | None) -> list[float] | None:
    if a is None or b is None:
        return a or b
    return [min(a[0], b[0]), min(a[1], b[1]), max(a[2], b[2]), max(a[3], b[3])]
