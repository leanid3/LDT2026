"""XML -> layout: каждая непустая текстовая нода — строка «путь: текст». Геометрии нет (bbox=None)."""

from __future__ import annotations

import xml.etree.ElementTree as ET
from pathlib import Path

from .layout import Line, Page, ParseResult, grade


def _local(tag: str) -> str:
    return tag.rsplit("}", 1)[-1]


def parse_xml(path: Path) -> ParseResult:
    lines: list[Line] = []
    stack: list[str] = []
    for event, elem in ET.iterparse(path, events=("start", "end")):
        if event == "start":
            stack.append(_local(elem.tag))
            continue
        text = " ".join((elem.text or "").split())
        if text:
            lines.append(Line(f"{'/'.join(stack[-3:])}: {text}", None))
        stack.pop()
        elem.clear()
    pages = [Page(page=1, method="xml", lines=lines)]
    quality, note = grade(pages, min_chars=1)
    return ParseResult(pages, quality, note)
