"""Контракт с оркестратором: общие golden-примеры contracts/events/examples проверяются здесь по JSON Schema
и в Go (backend/internal/mockworkers/contract_test.go) — расхождение сторон ломает сборку с любой стороны."""

from __future__ import annotations

import copy
import json

import pytest

from inspector_workers.contracts import ContractError, validate
from tests.conftest import CONTRACTS

EXAMPLES = CONTRACTS / "events" / "examples"
EVENTS = ["doc.parse.requested", "doc.parse.completed", "doc.extract.requested", "doc.extract.completed"]


def load(name: str) -> dict:
    return json.loads((EXAMPLES / f"{name}.json").read_text(encoding="utf-8"))


@pytest.mark.parametrize("event", EVENTS)
def test_golden_event_examples_are_valid(event):
    validate(CONTRACTS, event, load(event))


def test_golden_fact_example_is_valid():
    validate(CONTRACTS, "fact", load("fact"))


def _mut(doc: dict, path: str, value) -> dict:
    out = copy.deepcopy(doc)
    node = out
    *parents, last = path.split(".")
    for p in parents:
        node = node[p]
    if value is ...:
        del node[last]
    else:
        node[last] = value
    return out


@pytest.mark.parametrize("event,path,value", [
    ("doc.parse.requested", "data.storage_key", ...),                 # обязательное поле
    ("doc.parse.requested", "data.sha256", "not-a-hash"),
    ("doc.parse.requested", "data.page_from", 0),                     # страницы с 1
    ("doc.parse.requested", "event_type", "doc.parse.completed"),     # тип не тот
    ("doc.parse.requested", "process_id", "not-a-uuid"),
    ("doc.parse.completed", "data.quality", "GREAT"),
    ("doc.extract.requested", "data.params", ["X-1"]),
    ("doc.extract.completed", "data.facts_key", ...),
    ("doc.extract.completed", "schema_version", 2),
])
def test_invalid_events_are_rejected(event, path, value):
    with pytest.raises(ContractError):
        validate(CONTRACTS, event, _mut(load(event), path, value))


@pytest.mark.parametrize("path,value", [
    ("bbox", [0.1, 0.2, 1.4, 0.4]),          # вне [0;1]
    ("bbox", [0.1, 0.2, 0.3]),               # не 4 числа
    ("param_code", "KR-55"),                 # код не из Матрицы
    ("quote", ""),                           # без цитаты факт не доказательство
    ("stage", "XX"),
    ("confidence", 1.5),
    ("page", 0),
    ("method", "magic"),
])
def test_invalid_facts_are_rejected(path, value):
    with pytest.raises(ContractError):
        validate(CONTRACTS, "fact", _mut(load("fact"), path, value))


def test_fact_without_optional_fields_is_valid():
    fact = load("fact")
    for optional in ("process_id", "rule_code", "element_key", "value_norm", "bbox", "unit", "file_sha256"):
        fact.pop(optional, None)
    validate(CONTRACTS, "fact", fact)
