"""PDF -> layout. Текстовый слой читается pdfplumber (координаты слов -> bbox строк); страницы без
текста уходят в OCR, если он доступен."""

from __future__ import annotations

from pathlib import Path

import pdfplumber

from . import ocr
from .layout import MIN_TEXT_CHARS, Line, Page, ParseResult, grade, norm_bbox


def parse_pdf(path: Path, page_from: int | None, page_to: int | None, use_ocr: bool = True) -> ParseResult:
    pages: list[Page] = []
    with pdfplumber.open(path) as pdf:
        total = len(pdf.pages)
        first = max(1, page_from or 1)
        last = min(total, page_to or total)
        can_ocr = use_ocr and ocr.available()

        for number in range(first, last + 1):
            pg = pdf.pages[number - 1]
            width, height = float(pg.width), float(pg.height)
            lines = [
                Line(item["text"].strip(),
                     norm_bbox(item["x0"], item["top"], item["x1"], item["bottom"], width, height))
                for item in pg.extract_text_lines(layout=False, strip=True, return_chars=False)
                if item["text"].strip()
            ]
            page = Page(page=number, method="vector_text", lines=lines, width=width, height=height)
            if page.chars < MIN_TEXT_CHARS and can_ocr:
                ocr_lines = ocr.ocr_page(pg)
                if sum(len(ln.text) for ln in ocr_lines) > page.chars:
                    page = Page(page=number, method="ocr", lines=ocr_lines, width=width, height=height)
            pages.append(page)

    quality, note = grade(pages)
    return ParseResult(pages, quality, note)
