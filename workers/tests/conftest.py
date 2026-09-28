from __future__ import annotations

import json
import os
import shutil
import uuid
from pathlib import Path

import pytest

from inspector_workers.config import Settings

REPO = Path(__file__).resolve().parents[2]
CONTRACTS = REPO / "contracts"

_FONT_CANDIDATES = [
    os.environ.get("TEST_FONT", ""),
    "/usr/share/fonts/TTF/DejaVuSans.ttf",
    "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    "/usr/share/fonts/dejavu/DejaVuSans.ttf",
    "/usr/share/fonts/liberation/LiberationSans-Regular.ttf",
    "/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
]


def _font() -> str | None:
    return next((p for p in _FONT_CANDIDATES if p and Path(p).exists()), None)


@pytest.fixture(scope="session")
def make_pdf(tmp_path_factory):
    """make_pdf(pages: list[list[str]]) -> Path. Каждая строка — отдельная текстовая строка страницы;
    пустой список — страница без текстового слоя (имитация скана)."""
    font = _font()
    if font is None:
        pytest.skip("нет TTF-шрифта с кириллицей (задайте TEST_FONT)")
    from reportlab.lib.pagesizes import A4
    from reportlab.pdfbase import pdfmetrics
    from reportlab.pdfbase.ttfonts import TTFont
    from reportlab.pdfgen import canvas

    pdfmetrics.registerFont(TTFont("TestFont", font))

    def _make(pages: list[list[str]]) -> Path:
        path = tmp_path_factory.mktemp("pdf") / f"{uuid.uuid4().hex}.pdf"
        c = canvas.Canvas(str(path), pagesize=A4)
        for lines in pages:
            c.setFont("TestFont", 11)
            y = 800
            for text in lines:
                c.drawString(50, y, text)
                y -= 22
            c.showPage()
        c.save()
        return path

    return _make


@pytest.fixture
def settings() -> Settings:
    return Settings(
        db_dsn="", kafka_brokers="", minio_endpoint="", minio_access_key="", minio_secret_key="",
        minio_bucket="documents", minio_secure=False, contracts_dir=CONTRACTS, validate_events=True,
        max_attempts=2, ocr_enabled=False,
    )


class FakeStorage:
    """Подмена MinIO в юнит-тестах воркеров."""

    def __init__(self) -> None:
        self.files: dict[str, bytes] = {}
        self.json: dict[str, object] = {}
        self.jsonl: dict[str, list[dict]] = {}

    def download_to_temp(self, key: str, suffix: str = "") -> Path:
        import tempfile

        fd, name = tempfile.mkstemp(suffix=suffix)
        os.close(fd)
        Path(name).write_bytes(self.files[key])
        return Path(name)

    def get_json(self, key: str):
        return json.loads(json.dumps(self.json[key]))

    def put_json(self, key: str, doc) -> None:
        self.json[key] = json.loads(json.dumps(doc, ensure_ascii=False))

    def put_jsonl(self, key: str, rows: list[dict]) -> None:
        self.jsonl[key] = json.loads(json.dumps(rows, ensure_ascii=False))


@pytest.fixture
def storage() -> FakeStorage:
    return FakeStorage()
