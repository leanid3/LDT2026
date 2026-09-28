"""DOCX -> layout. У формата нет постраничной геометрии: одна логическая страница, bbox=None
(extract-воркер ставит для таких фактов bbox всей страницы [0,0,1,1] — «положение неизвестно»)."""

from __future__ import annotations

from pathlib import Path

from docx import Document
from docx.table import Table
from docx.text.paragraph import Paragraph

from .layout import Line, Page, ParseResult, grade


def parse_docx(path: Path) -> ParseResult:
    doc = Document(str(path))
    lines: list[Line] = []
    for block in doc.iter_inner_content():
        if isinstance(block, Paragraph):
            text = block.text.strip()
            if text:
                lines.append(Line(text, None))
        elif isinstance(block, Table):
            for row in block.rows:
                cells: list[str] = []
                seen = set()
                for cell in row.cells:
                    if id(cell._tc) in seen:  # объединённые ячейки python-docx повторяет
                        continue
                    seen.add(id(cell._tc))
                    t = " ".join(cell.text.split())
                    if t:
                        cells.append(t)
                if cells:
                    lines.append(Line(" | ".join(cells), None))
    pages = [Page(page=1, method="docx", lines=lines)]
    quality, note = grade(pages, min_chars=1)
    return ParseResult(pages, quality, note)
