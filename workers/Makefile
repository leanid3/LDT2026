.PHONY: venv test run-parse run-extract

PY := .venv/bin/python

venv: ## Создать .venv и поставить воркеры с dev-зависимостями
	python3 -m venv .venv
	$(PY) -m pip install -q -e ".[dev]"

test: ## Юнит-тесты (без docker). Интеграционные — с TEST_DATABASE_DSN, см. tests/test_bus_integration.py
	$(PY) -m pytest tests -q

run-parse: ## parse-воркер (doc.parse.requested -> layout в MinIO -> doc.parse.completed)
	$(PY) -m inspector_workers parse

run-extract: ## extract-воркер (doc.extract.requested -> факты). LLM: LLM_BASE_URL, LLM_MODEL, LLM_API_KEY
	$(PY) -m inspector_workers extract
