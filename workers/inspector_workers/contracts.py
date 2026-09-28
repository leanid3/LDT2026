"""Валидация событий и фактов по JSON Schema из contracts/ — тот же контракт, что проверяет Go
(backend/internal/mockworkers/contract_test.go на общих примерах contracts/events/examples)."""

from __future__ import annotations

import json
from functools import lru_cache
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker


class ContractError(ValueError):
    """Сообщение не соответствует контракту."""


@lru_cache(maxsize=None)
def _validator(contracts_dir: Path, name: str) -> Draft202012Validator:
    path = contracts_dir / ("facts.schema.json" if name == "fact" else f"events/{name}.schema.json")
    schema = json.loads(path.read_text(encoding="utf-8"))
    return Draft202012Validator(schema, format_checker=FormatChecker())


def validate(contracts_dir: Path, name: str, document: dict) -> None:
    """name — event_type ('doc.parse.requested', ...) или 'fact'. Бросает ContractError."""
    errors = sorted(_validator(contracts_dir, name).iter_errors(document), key=lambda e: list(e.path))
    if errors:
        e = errors[0]
        where = "/".join(str(p) for p in e.path) or "<root>"
        raise ContractError(f"{name}: {where}: {e.message}")
