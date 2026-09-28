from __future__ import annotations

import pytest

from inspector_workers.config import Settings
from inspector_workers.extract.catalog import load_catalog
from inspector_workers.extract.heuristic import extract_from_pages
from inspector_workers.extract import normalize


@pytest.fixture(scope="module")
def catalog():
    from tests.conftest import CONTRACTS
    return load_catalog(CONTRACTS)


def run(catalog, *texts: str, page: int = 1):
    pages = [{"page": page, "lines": [{"text": t, "bbox": [0.1, 0.1 + i * 0.05, 0.9, 0.14 + i * 0.05]} for i, t in enumerate(texts)]}]
    return {c.param_code: c for c in extract_from_pages(pages, catalog)}


def test_catalog_has_132(catalog):
    assert len(catalog) == 132 and catalog["M-055"].scale == "concrete"


def test_every_pattern_targets_a_real_matrix_code(catalog):
    from inspector_workers.extract.patterns import PATTERNS
    assert {p.code for p in PATTERNS} <= set(catalog)


@pytest.mark.parametrize("text,code,value,unit", [
    ("Площадь застройки — 1 250,5 м²", "M-001", 1250.5, "м²"),
    ("Площадь застройки: 1250.5 кв.м", "M-001", 1250.5, "м²"),
    ("Общая площадь здания составляет 8 400 м2", "M-002", 8400.0, "м²"),
    ("Строительный объем, всего — 45 000 м³", "M-004", 45000.0, "м³"),
    ("Строительный объём подземной части 5 000 м³", "M-005", 5000.0, "м³"),
    ("Количество этажей: 9", "M-007", 9.0, "ед."),
    ("Высота здания 28,5 м", "M-008", 28.5, "м"),
    ("Количество квартир — 120", "M-010", 120.0, "шт."),
    ("Ширина двери 0,8 м", "M-041", 0.8, "м"),
    ("Ширина эвакуационных выходов 900 мм", "M-041", 0.9, "м"),  # мм -> м (единица Матрицы)
    ("Высота порога 14 мм", "M-118", 0.014, "м"),
    ("Ширина проезда 3,5 м", "M-030", 3.5, "м"),
    ("Ширина эвакуационного коридора 1,4 м", "M-040", 1.4, "м"),
    ("Толщина фундаментной плиты 800 мм", "M-058", 800.0, "мм"),
    ("Толщина утеплителя наружных стен 150 мм", "M-125", 150.0, "мм"),
    ("Толщина утеплителя кровли 200 мм", "M-128", 200.0, "мм"),
    ("Коэффициент застройки 0,32 %", "M-019", 0.32, "%"),
])
def test_numeric_extraction_and_unit_conversion(catalog, text, code, value, unit):
    c = run(catalog, text)[code]
    assert c.value_norm["kind"] == "number"
    assert c.value_norm["value"] == pytest.approx(value)
    assert c.value_norm["unit_si"] == unit
    assert c.quote == text and c.method == "regex" and 0 < c.confidence <= 1


@pytest.mark.parametrize("text,code,canon", [
    ("Бетон класса В30 W6 F150", "M-055", "B30"),
    ("Класс бетона по прочности на сжатие B25", "M-055", "B25"),
    ("Арматура класса А500С", "M-057", "A500C"),
    ("Сталь С345", "M-056", "C345"),
    ("Степень огнестойкости здания II", "M-022", "II"),
    ("Класс конструктивной пожарной опасности С0", "M-023", "C0"),
    ("Класс энергетической эффективности здания B", "M-021", "B"),
    ("Класс энергетической эффективности: A++", "M-021", "A++"),
    ("Категория надежности электроснабжения I", "M-015", "I"),
    ("Отделка путей эвакуации материалами КМ1", "M-107", "KM1"),
])
def test_ordinal_extraction_canonicalises(catalog, text, code, canon):
    c = run(catalog, text)[code]
    assert c.value_norm == {"kind": "ordinal", "value": canon, "scale": catalog[code].scale}


def test_no_false_positives_on_prose(catalog):
    found = run(catalog, "Пояснительная записка к проекту.", "Здание расположено на участке по адресу: г. Москва.",
                "Настоящий раздел разработан в соответствии с заданием на проектирование.",
                "Стальные конструкции окрашены. Бетонные работы выполнены в 2 этапа.")
    assert found == {}, f"ложные срабатывания: {found}"


def test_general_volume_does_not_swallow_underground(catalog):
    found = run(catalog, "Строительный объём подземной части 5 000 м³")
    assert set(found) == {"M-005"}, "подземный объём не должен превращаться ещё и в общий M-004"


def test_insulation_of_roof_is_not_wall_insulation(catalog):
    assert set(run(catalog, "Толщина утеплителя кровли 200 мм")) == {"M-128"}


def test_label_and_value_on_adjacent_lines(catalog):
    c = run(catalog, "Общая площадь здания", "8 400 м²")["M-002"]
    assert c.value_norm["value"] == 8400.0
    assert c.quote == "Общая площадь здания 8 400 м²"
    assert c.bbox[1] == pytest.approx(0.1) and c.bbox[3] == pytest.approx(0.19), "bbox — объединение двух строк"


def test_unit_missing_lowers_confidence(catalog):
    with_unit = run(catalog, "Высота здания 28,5 м")["M-008"].confidence
    without = run(catalog, "Высота здания 28,5")["M-008"].confidence
    assert without < with_unit


def test_normalize_helpers():
    assert normalize.parse_number("1 250,5 м²") == 1250.5
    assert normalize.to_matrix_unit(900, "мм", "м") == (0.9, "м")
    assert normalize.to_matrix_unit(1.2, "м", "мм") == (1200.0, "мм")
    assert normalize.to_matrix_unit(5, "шт", "шт.")[1] == "шт.", "единица результата — как в Матрице"
    assert normalize.canon_unit("кв. м.") == "м²"
    assert normalize.ordinal_canon("а500с") == "A500C"
    with pytest.raises(ValueError):
        normalize.parse_number("нет")


@pytest.mark.parametrize("text", [
    "Бетон в 2 слоя",                       # «в 2» — предлог + число, не класс В2
    "Сталь используется в 345 позициях",     # «в 345» — не марка С345
    "Арматура а 400 штук поставлена",        # «а 400» — союз, не класс А400
])
def test_ordinal_patterns_do_not_fire_on_prepositions(catalog, text):
    assert run(catalog, text) == {}


def test_label_inside_longer_word_is_not_a_match(catalog):
    """«бетон» внутри «железобетонные» — не подпись параметра; доказательство не должно захватывать лишнюю строку."""
    found = run(catalog, "Раздел КЖ. Конструкции железобетонные", "Бетон монолитных конструкций класса В30 W6 F150")
    c = found["M-055"]
    assert c.quote == "Бетон монолитных конструкций класса В30 W6 F150"
    assert c.bbox == pytest.approx([0.1, 0.15, 0.9, 0.19]), "bbox только второй строки, без первой"


def test_single_line_match_beats_spanning_window(catalog):
    single = run(catalog, "Общая площадь здания: 8 400 м²")["M-002"].confidence
    spanning = run(catalog, "Общая площадь здания", "8 400 м²")["M-002"].confidence
    assert spanning < single
