"""Layout — результат парсинга: страницы -> строки с нормализованным bbox [x0,y0,x1,y1] в [0;1]
(начало координат — левый верхний угол). Это вход extract-воркера и источник bbox для доказательств."""

from __future__ import annotations

from dataclasses import dataclass, field

LAYOUT_VERSION = 1
MIN_TEXT_CHARS = 30  # меньше — на странице нет пригодного текстового слоя (скан/пустой лист)

_EPS = 1e-4


@dataclass
class Line:
    text: str
    bbox: list[float] | None  # None — у формата нет геометрии (DOCX/XML)

    def to_json(self) -> dict:
        return {"text": self.text, "bbox": self.bbox}


@dataclass
class Page:
    page: int
    method: str  # vector_text | ocr | docx | xml
    lines: list[Line] = field(default_factory=list)
    width: float | None = None
    height: float | None = None

    @property
    def chars(self) -> int:
        return sum(len(line.text) for line in self.lines)

    def to_json(self) -> dict:
        return {"page": self.page, "method": self.method, "width": self.width, "height": self.height,
                "chars": self.chars, "lines": [line.to_json() for line in self.lines]}


def norm_bbox(x0: float, y0: float, x1: float, y1: float, width: float, height: float) -> list[float]:
    """Координаты страницы -> [0;1], с гарантией x0<x1, y0<y1 (контракт facts.schema.json)."""

    def c(v: float) -> float:
        return min(1.0, max(0.0, v))

    nx0, ny0, nx1, ny1 = c(x0 / width), c(y0 / height), c(x1 / width), c(y1 / height)
    if nx1 - nx0 < _EPS:
        nx0, nx1 = (nx0 - _EPS, nx0) if nx0 >= 1 - _EPS else (nx0, nx0 + _EPS)
    if ny1 - ny0 < _EPS:
        ny0, ny1 = (ny0 - _EPS, ny0) if ny0 >= 1 - _EPS else (ny0, ny0 + _EPS)
    return [round(c(nx0), 5), round(c(ny0), 5), round(c(nx1), 5), round(c(ny1), 5)]


@dataclass
class ParseResult:
    pages: list[Page]
    quality: str  # OK | LOW_QUALITY | ABSTAIN | FAILED
    note: str = ""

    @property
    def stats(self) -> dict:
        return {
            "pages": len(self.pages),
            "chars": sum(p.chars for p in self.pages),
            "text_pages": sum(1 for p in self.pages if p.chars >= MIN_TEXT_CHARS),
            "ocr_pages": sum(1 for p in self.pages if p.method == "ocr"),
            "note": self.note,
        }


def grade(pages: list[Page], min_chars: int = MIN_TEXT_CHARS) -> tuple[str, str]:
    """Качество результата по доле страниц с текстом. min_chars=1 для DOCX/XML: у них нет сканов,
    порог «есть ли текстовый слой» имеет смысл только для PDF."""
    if not pages:
        return "FAILED", "в заданном диапазоне нет страниц"
    text_pages = sum(1 for p in pages if p.chars >= min_chars)
    if text_pages == 0:
        return "ABSTAIN", "нет текстового слоя и OCR недоступен или ничего не распознал"
    if text_pages < len(pages):
        return "LOW_QUALITY", f"{len(pages) - text_pages} из {len(pages)} страниц без текста"
    return "OK", ""
