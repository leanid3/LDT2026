"""Каталог параметров Матрицы из contracts/matrix.json (генерируется backend/cmd/tools/import-matrix)."""

from __future__ import annotations

import json
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path


@dataclass(frozen=True)
class Param:
    code: str
    name: str
    unit: str
    data_type: str  # number | ordinal | string
    scale: str | None
    rule_type: str | None
    section: str = ""


@lru_cache(maxsize=None)
def load_catalog(contracts_dir: Path) -> dict[str, Param]:
    doc = json.loads((contracts_dir / "matrix.json").read_text(encoding="utf-8"))
    return {
        p["code"]: Param(p["code"], p["name"], p.get("unit", ""), p.get("data_type", "string"),
                         p.get("scale") or None, p.get("rule_type") or None, p.get("section", ""))
        for p in doc["params"]
    }
