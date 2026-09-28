"""Опциональный OCR для страниц без текстового слоя (сканы). Требует tesseract + pytesseract
(`pip install .[ocr]`, пакеты tesseract-ocr и tesseract-ocr-rus). Без них parse честно помечает такие
страницы, а не выдумывает текст."""

from __future__ import annotations

import shutil
from typing import Any

from .layout import Line, norm_bbox

OCR_DPI = 300
OCR_LANG = "rus+eng"


def available() -> bool:
    if shutil.which("tesseract") is None:
        return False
    try:
        import pytesseract  # noqa: F401
    except ImportError:
        return False
    return True


def ocr_page(pdf_page: Any) -> list[Line]:
    """Распознаёт страницу pdfplumber, возвращает строки с нормализованным bbox."""
    import pytesseract

    img = pdf_page.to_image(resolution=OCR_DPI).original
    data = pytesseract.image_to_data(img, lang=OCR_LANG, output_type=pytesseract.Output.DICT)
    width, height = img.size

    grouped: dict[tuple[int, int, int], list[tuple[str, int, int, int, int]]] = {}
    for i, word in enumerate(data["text"]):
        word = word.strip()
        if not word or float(data["conf"][i]) < 0:
            continue
        key = (data["block_num"][i], data["par_num"][i], data["line_num"][i])
        x, y, w, h = data["left"][i], data["top"][i], data["width"][i], data["height"][i]
        grouped.setdefault(key, []).append((word, x, y, x + w, y + h))

    lines: list[Line] = []
    for words in grouped.values():
        text = " ".join(w[0] for w in words)
        lines.append(Line(text, norm_bbox(min(w[1] for w in words), min(w[2] for w in words),
                                          max(w[3] for w in words), max(w[4] for w in words), width, height)))
    lines.sort(key=lambda ln: (ln.bbox[1], ln.bbox[0]) if ln.bbox else (0, 0))
    return lines
