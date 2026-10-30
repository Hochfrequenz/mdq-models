"""Python models against the shared fixtures and the Go golden file; must stay consistent with golden_test.go."""

import json
import subprocess
import sys
from datetime import UTC, datetime, timedelta, timezone
from decimal import Decimal, localcontext
from pathlib import Path
from typing import Any

import pytest
from pydantic import BaseModel, TypeAdapter, ValidationError

from mdq_models import (
    Herkunft,
    Messwert,
    Profiltyp,
    Richtung,
    Status,
    Zaehlerstand,
    check_json,
    herkunft_passt_zu_status,
    is_valid_malo_id,
    malo_check_digit,
)

TESTDATA = Path(__file__).resolve().parent.parent / "testdata"
ENCODING_FIXTURES = {"utf8_bom.json", "utf16_encoded.json", "invalid_utf8_bytes.json"}
MODELS: dict[str, type[BaseModel]] = {"messwert": Messwert, "zaehlerstand": Zaehlerstand}


def _files(validity: str) -> list[Path]:
    return [p for kind in MODELS for p in sorted((TESTDATA / kind / validity).glob("*.json"))]


def _name(path: Path) -> str:
    """``messwert/x.json``, the key used in serialized.golden and invalid_reasons.json."""
    return f"{path.parent.parent.name}/{path.name}"


def _golden() -> dict[str, str]:
    lines = (TESTDATA / "serialized.golden").read_text(encoding="utf-8").splitlines()
    golden: dict[str, str] = {}
    for line in lines:
        name, json_text = line.split("\t", 1)
        assert name not in golden
        golden[name] = json_text
    return golden


def _shared(name: str) -> Any:
    return json.loads((TESTDATA / name).read_bytes())


def _valid_messwert(**overrides: Any) -> dict[str, Any]:
    data: dict[str, Any] = {
        "malo": "68283561554",
        "timestamp": "2026-01-15T10:15:00Z",
        "resolution": "PT15M",
        "value": "1.5",
        "status": "wahr",
        "herkunft": "fremd",
        "profiltyp": "haushalt",
        "richtung": "bezug",
    }
    data.update(overrides)
    return data


def _valid_zaehlerstand(**overrides: Any) -> dict[str, Any]:
    data: dict[str, Any] = {"malo": "68283561554", "timestamp": "2026-01-15T10:17:23Z", "value": "1.5"}
    data.update(overrides)
    return data


def test_golden_file_lists_exactly_the_valid_fixtures() -> None:
    assert sorted(_golden()) == sorted(_name(p) for p in _files("valid"))


@pytest.mark.parametrize("path", _files("valid"), ids=_name)
def test_valid_fixture_parses_and_serialises_to_the_golden_bytes(path: Path) -> None:
    model = MODELS[path.parent.parent.name].model_validate_json(path.read_bytes())
    assert model.model_dump_json() == _golden()[_name(path)]


@pytest.mark.parametrize("path", _files("valid"), ids=_name)
def test_golden_output_parses_back_to_an_equal_model(path: Path) -> None:
    cls = MODELS[path.parent.parent.name]
    assert cls.model_validate_json(_golden()[_name(path)]) == cls.model_validate_json(path.read_bytes())


@pytest.mark.parametrize("path", _files("invalid"), ids=_name)
def test_invalid_fixture_is_rejected(path: Path) -> None:
    with pytest.raises(ValueError):  # ValidationError is a ValueError, and so is the duplicate-key error
        MODELS[path.parent.parent.name].model_validate_json(path.read_bytes())


@pytest.mark.parametrize("path", _files("invalid"), ids=_name)
def test_invalid_fixture_is_rejected_from_a_python_dict_too(path: Path) -> None:
    """Building in code applies the same rules (duplicate keys cannot occur in a dict)."""
    if path.name == "duplicate_key.json":
        return
    if path.name in ENCODING_FIXTURES:
        return  # about bytes, not content; test_invalid_fixture_is_rejected covers them (json.loads decodes them)
    doc = json.loads(path.read_bytes())
    if not isinstance(doc, dict):
        return
    with pytest.raises(ValueError):
        MODELS[path.parent.parent.name].model_validate(doc)


def test_duplicate_keys_are_rejected_at_every_nesting_level() -> None:
    with pytest.raises(ValidationError, match="duplicate"):
        Messwert.model_validate_json(json.dumps(_valid_messwert()).replace('"PT15M"', '"PT15M", "malo": "68283561554"'))
    with pytest.raises(ValidationError, match="duplicate"):
        Zaehlerstand.model_validate_json('{"x": {"a": 1, "a": 2}}')


def test_duplicate_keys_are_rejected_even_if_the_values_are_equal() -> None:
    raw = '{"malo":"68283561554","malo":"68283561554","timestamp":"2026-01-15T10:17:23Z","value":"1"}'
    with pytest.raises(ValidationError, match="duplicate"):
        Zaehlerstand.model_validate_json(raw)


def test_non_utf8_bytes_and_nan_literals_are_rejected_as_value_errors() -> None:
    with pytest.raises(ValueError):
        Zaehlerstand.model_validate_json(b"\xff")
    with pytest.raises(ValueError):
        Zaehlerstand.model_validate_json('{"malo":"68283561554","timestamp":"2026-01-15T10:17:23Z","value":NaN}')


# --- Decimal ----------------------------------------------------------------------------------------------------


@pytest.mark.parametrize(
    ("given", "written"),
    [
        (Decimal("1.5000000"), "1.5"),  # trailing zeros are stripped before the decimal places are counted
        (Decimal("1.50"), "1.5"),
        (Decimal("100"), "100"),
        (Decimal("1E+3"), "1000"),
        (Decimal("0.000001"), "0.000001"),
        (Decimal("0E-10"), "0"),
        (Decimal("0.0"), "0"),
        (Decimal("-0"), "0"),  # negative zero has no canonical spelling; it is the number zero
        (Decimal("-0.5"), "-0.5"),
        (Decimal("999999999999.999999"), "999999999999.999999"),
    ],
)
def test_decimal_built_in_code_is_normalised(given: Decimal, written: str) -> None:
    z = Zaehlerstand(malo="68283561554", timestamp=datetime(2026, 1, 1, tzinfo=UTC), value=given)
    assert json.loads(z.model_dump_json())["value"] == written
    assert z.value == given


def test_decimal_is_not_rounded_to_the_context_precision() -> None:
    long = Decimal("999999999999.999999")  # 18 significant digits; exact under a small context too
    with localcontext() as ctx:
        ctx.prec = 5
        z = Zaehlerstand(malo="68283561554", timestamp=datetime(2026, 1, 1, tzinfo=UTC), value=long)
        assert json.loads(z.model_dump_json())["value"] == "999999999999.999999"


@pytest.mark.parametrize("bad", [Decimal("1E+12"), Decimal("1000000000000"), Decimal("-1000000000000.5")])
def test_decimal_with_more_than_12_integer_digits_is_rejected(bad: Decimal) -> None:
    with pytest.raises(ValidationError):
        Zaehlerstand(malo="68283561554", timestamp=datetime(2026, 1, 1, tzinfo=UTC), value=bad)
    with pytest.raises(ValidationError):
        Zaehlerstand.model_validate(_valid_zaehlerstand(value=format(bad, "f")))


@pytest.mark.parametrize(
    "bad",
    [Decimal("1.0000001"), Decimal("0.1234567"), Decimal("-0.0000001"), Decimal("1E-7"), Decimal("NaN"),
     Decimal("sNaN"), Decimal("Infinity"), Decimal("-Infinity"), Decimal("1E+999999999"), 1.5, 1, True, None],
)  # fmt: skip
def test_decimal_built_in_code_is_rejected(bad: Any) -> None:
    with pytest.raises(ValidationError):
        Zaehlerstand(malo="68283561554", timestamp=datetime(2026, 1, 1, tzinfo=UTC), value=bad)


@pytest.mark.parametrize(
    "bad", ["1.0", "01", "+1", "1e3", "-0", ".5", "5.", "NaN", "0.1234567", "1\n", "1\u0661", "1_000", ""]
)
def test_decimal_string_must_be_canonical(bad: str) -> None:
    with pytest.raises(ValidationError):
        Zaehlerstand.model_validate(_valid_zaehlerstand(value=bad))


# --- timestamps -------------------------------------------------------------------------------------------------


def test_aware_datetime_in_another_zone_is_normalised_to_utc() -> None:
    berlin = timezone(timedelta(hours=1))
    m = Messwert.model_validate(_valid_messwert(timestamp=datetime(2026, 1, 15, 11, 15, tzinfo=berlin)))
    assert m.timestamp == datetime(2026, 1, 15, 10, 15, tzinfo=UTC)
    assert m.timestamp.utcoffset() == timedelta(0)
    assert m.model_dump_json().count('"timestamp":"2026-01-15T10:15:00Z"') == 1


def test_naive_datetime_is_rejected() -> None:
    with pytest.raises(ValidationError):
        Messwert.model_validate(_valid_messwert(timestamp=datetime(2026, 1, 15, 10, 15)))
    with pytest.raises(ValidationError):
        Zaehlerstand.model_validate(_valid_zaehlerstand(timestamp=datetime(2026, 1, 15, 10, 15)))


@pytest.mark.parametrize(
    "bad",
    [
        datetime(2026, 1, 15, 10, 7, tzinfo=UTC),  # off the raster
        datetime(2026, 1, 15, 10, 15, 30, tzinfo=UTC),
        datetime(2026, 1, 15, 10, 15, 0, 1, tzinfo=UTC),  # sub-second
        datetime(1899, 12, 31, 23, 45, tzinfo=UTC),
        datetime(9999, 12, 31, 23, 45, tzinfo=timezone(timedelta(hours=-1))),  # 10000 in UTC, cannot be represented
        "2026-01-15T11:15:00+01:00",
        1768472100,
        None,
    ],
)
def test_timestamp_built_in_code_is_rejected(bad: Any) -> None:
    with pytest.raises(ValidationError):
        Messwert.model_validate(_valid_messwert(timestamp=bad))


def test_zaehlerstand_accepts_any_whole_second_but_not_sub_seconds() -> None:
    Zaehlerstand.model_validate(_valid_zaehlerstand(timestamp=datetime(2026, 1, 15, 10, 7, 9, tzinfo=UTC)))
    with pytest.raises(ValidationError):
        Zaehlerstand.model_validate(_valid_zaehlerstand(timestamp=datetime(2026, 1, 15, 10, 7, 9, 5, tzinfo=UTC)))


# --- model rules ------------------------------------------------------------------------------------------------


def test_building_in_code_with_enums_and_decimals() -> None:
    m = Messwert(
        malo="68283561554",
        timestamp=datetime(2026, 1, 15, 10, 15, tzinfo=UTC),
        resolution="PT15M",
        value=Decimal("1.50"),
        status=Status.VORLAEUFIG,
        herkunft=Herkunft.MQA,
        profiltyp=Profiltyp.GEWERBE,
        richtung=Richtung.EINSPEISUNG,
    )
    assert m.model_dump_json() == (
        '{"malo":"68283561554","timestamp":"2026-01-15T10:15:00Z","resolution":"PT15M","value":"1.5",'
        '"status":"vorlaeufig","herkunft":"mqa","profiltyp":"gewerbe","richtung":"einspeisung"}'
    )


def test_fehlend_is_written_with_explicit_nulls() -> None:
    m = Messwert.model_validate(_valid_messwert(status="fehlend", value=None, herkunft=None))
    assert '"value":null,"status":"fehlend","herkunft":null' in m.model_dump_json()


@pytest.mark.parametrize(
    "overrides",
    [
        {"status": "fehlend"},  # value and herkunft set
        {"status": "fehlend", "value": None},  # herkunft set
        {"status": "fehlend", "herkunft": None},  # value set
        {"value": None},
        {"herkunft": None},
        {"status": "wahr", "herkunft": "mqa"},
        {"status": "gemessen"},
        {"status": "Wahr"},
        {"resolution": "PT5M"},
        {"unknown": 1},
        {"profiltyp": 1},
        {"malo": 68283561554},
        {"malo": "68283561555"},
    ],
)
def test_messwert_rules_hold_when_built_in_code(overrides: dict[str, Any]) -> None:
    with pytest.raises(ValidationError):
        Messwert.model_validate(_valid_messwert(**overrides))


@pytest.mark.parametrize(
    "key", ["malo", "timestamp", "resolution", "value", "status", "herkunft", "profiltyp", "richtung"]
)
def test_every_messwert_key_is_required_even_if_nullable(key: str) -> None:
    data = _valid_messwert(status="fehlend", value=None, herkunft=None)
    del data[key]
    with pytest.raises(ValidationError):
        Messwert.model_validate(data)


def test_models_are_frozen() -> None:
    m = Messwert.model_validate(_valid_messwert())
    with pytest.raises(ValidationError):
        m.malo = "52291008369"  # type: ignore[misc]
    assert hash(m) == hash(Messwert.model_validate(_valid_messwert()))


def test_every_field_has_a_description() -> None:
    for cls in (Messwert, Zaehlerstand):
        for name, field in cls.model_fields.items():
            assert field.description, f"{cls.__name__}.{name}"


def test_enum_values_match_the_proto_enums_file() -> None:
    enums = _shared("proto_enums.json")
    for cls, name in ((Status, "Status"), (Herkunft, "Herkunft"), (Profiltyp, "Profiltyp"), (Richtung, "Richtung")):
        assert [m.value for m in cls] == [v["json"] for v in enums[name]["values"]]


# --- shared cases -----------------------------------------------------------------------------------------------


def test_malo_ids_file() -> None:
    cases = _shared("malo_ids.json")
    for malo in cases["valid"]:
        assert is_valid_malo_id(malo), malo
    for case in cases["invalid"]:
        assert not is_valid_malo_id(case["id"]), case
    for case in cases["check_digits"]:
        assert malo_check_digit(case["first_ten"]) == case["check_digit"], case
    for case in cases["check_digit_invalid_input"]:
        with pytest.raises(ValueError):
            malo_check_digit(case["first_ten"])


def test_malo_id_with_trailing_newline_is_invalid() -> None:
    assert not is_valid_malo_id("68283561554\n")  # `$` would match before the newline; fullmatch does not
    with pytest.raises(ValueError):
        malo_check_digit("6828356155\n")


def test_herkunft_status_file() -> None:
    cases = _shared("herkunft_status.json")
    assert len(cases) > 12
    for case in cases:
        assert herkunft_passt_zu_status(case["status"], case["herkunft"]) is case["ok"], case


def test_herkunft_status_accepts_enum_members() -> None:
    assert herkunft_passt_zu_status(Status.WAHR, Herkunft.FREMD)
    assert not herkunft_passt_zu_status(Status.WAHR, Herkunft.MQA)
    assert herkunft_passt_zu_status(Status.FEHLEND, None)


def test_importing_the_models_does_not_import_polars() -> None:
    code = "import sys, mdq_models; sys.exit('polars' in sys.modules)"
    assert subprocess.run([sys.executable, "-c", code], check=False).returncode == 0


@pytest.mark.parametrize(
    "raw",
    [
        b"\xef\xbb\xbf" + json.dumps(_valid_zaehlerstand()).encode(),  # UTF-8 BOM
        "﻿" + json.dumps(_valid_zaehlerstand()),
        json.dumps(_valid_zaehlerstand()).encode("utf-16"),  # json.loads would accept these
        json.dumps(_valid_zaehlerstand()).encode("utf-16-le"),
        json.dumps(_valid_zaehlerstand()).encode("utf-32"),
        b'{"malo":"68283561554","timestamp":"2026-01-15T10:17:23Z","value":"1\xff"}',  # invalid UTF-8
    ],
)
def test_only_utf8_without_bom_is_accepted(raw: bytes | str) -> None:
    with pytest.raises(ValueError):
        Zaehlerstand.model_validate_json(raw)


def test_huge_json_number_is_rejected_because_value_must_be_a_string() -> None:
    raw = '{"malo":"68283561554","timestamp":"2026-01-15T10:17:23Z","value":1e1000}'
    with pytest.raises(ValidationError) as info:
        Zaehlerstand.model_validate_json(raw)
    assert info.value.errors()[0]["loc"] == ("value",)


def test_huge_json_integer_is_rejected() -> None:
    raw = f'{{"malo":"68283561554","timestamp":"2026-01-15T10:17:23Z","value":{"1" * 5000}}}'
    with pytest.raises(ValidationError):  # pydantic's own parser refuses it before field validation
        Zaehlerstand.model_validate_json(raw)


def _error_type(raw: str | bytes, model: type[BaseModel] = Zaehlerstand) -> str:
    with pytest.raises(ValidationError) as info:
        model.model_validate_json(raw)
    return info.value.errors()[0]["type"]


def test_every_parse_failure_is_a_pydantic_validation_error() -> None:
    ok = '"malo":"68283561554","timestamp":"2026-01-15T10:17:23Z","value":"1"'
    assert _error_type(f'{{{ok},"value":"1"}}') == "duplicate_key"
    assert _error_type("{") == "json_invalid"
    assert _error_type("") == "json_invalid"
    assert _error_type(f'{{{ok},"x":NaN}}') == "json_invalid"
    assert _error_type(b"\xff") == "not_utf8"
    assert _error_type(b"\xef\xbb\xbf{}") == "byte_order_mark"
    assert _error_type("[]") != ""  # top level not an object: pydantic's own error


def test_deeply_nested_document_is_a_validation_error_not_a_recursion_error() -> None:
    raws = ("[" * 100_000 + "]" * 100_000, '{"a":' * 100_000 + "1" + "}" * 100_000, "[" * 101 + "]" * 101)
    for raw in raws:
        assert _error_type(raw) == "too_deep"
        with pytest.raises(ValidationError):
            check_json(raw)


def test_check_json_guards_other_entry_points() -> None:
    raw = '{"malo":"68283561554","malo":"68283561554","timestamp":"2026-01-15T10:17:23Z","value":"1"}'
    TypeAdapter(Zaehlerstand).validate_json(raw)  # the documented limit: pydantic keeps the last duplicate
    with pytest.raises(ValidationError, match="duplicate"):
        check_json(raw)
    check_json(b'{"a": [1, {"b": 2}]}')


@pytest.mark.parametrize("offset", [timedelta(microseconds=1), timedelta(microseconds=-500_000)])
def test_offset_with_a_sub_second_part_is_rejected(offset: timedelta) -> None:
    ts = datetime(2026, 1, 15, 10, 15, tzinfo=timezone(offset))  # whole seconds locally, microseconds in UTC
    with pytest.raises(ValidationError, match="whole seconds"):
        Messwert.model_validate(_valid_messwert(timestamp=ts))
    with pytest.raises(ValidationError, match="whole seconds"):
        Zaehlerstand.model_validate(_valid_zaehlerstand(timestamp=ts))


def test_model_dump_keeps_python_types_and_json_dump_writes_strings() -> None:
    m = Messwert.model_validate(_valid_messwert())
    dumped = m.model_dump()
    assert dumped["value"] == Decimal("1.5")
    assert isinstance(dumped["timestamp"], datetime)
    assert json.loads(m.model_dump_json())["value"] == "1.5"


@pytest.mark.parametrize("field", ["status", "herkunft", "profiltyp", "richtung"])
@pytest.mark.parametrize("bad", [b"wahr", 1, b"fremd"])
def test_enum_fields_refuse_bytes_and_numbers_in_code(field: str, bad: Any) -> None:
    with pytest.raises(ValidationError):
        Messwert.model_validate(_valid_messwert(**{field: bad}))


@pytest.mark.parametrize(
    ("status", "herkunft"),
    [
        ("Wahr", "fremd"),
        ("gemessen", None),
        ("foo", "mqa"),
        ("vorlaeufig", ""),
        ("vorlaeufig", "bogus"),
        ("fehlend", ""),
    ],
)
def test_herkunft_passt_zu_status_rejects_unknown_values(status: str, herkunft: str | None) -> None:
    assert herkunft_passt_zu_status(status, herkunft) is False


def test_nesting_limit_boundary_and_brackets_inside_strings() -> None:
    check_json("[" * 100 + "]" * 100)  # at the limit
    check_json('{"a": "[[[[[[[[[[ \\" ]]]]"}')  # brackets and escaped quotes inside a string do not count
    check_json("[" + "[" * 99 + "]" * 99 + "," + "[" * 99 + "]" * 99 + "]")  # depth, not total count


def test_messwert_validation_and_output_ignore_the_ambient_decimal_context() -> None:
    """scalars.py works on the digit tuple and strings, so a tiny precision and trapped signals change nothing."""
    from decimal import Context, Inexact, InvalidOperation, Rounded  # noqa: PLC0415

    fields: dict[str, Any] = json.loads((TESTDATA / "messwert" / "valid" / "wahr_fremd.json").read_text())
    with localcontext(Context(prec=5, traps=[InvalidOperation, Inexact, Rounded])):
        for text in ("999999999999.999999", "-999999999999.999999", "0.000001", "123456.654321"):
            row = Messwert.model_validate({**fields, "value": text})
            assert row.value == Decimal(text)
            assert f'"value":"{text}"' in row.model_dump_json()
        assert Messwert.model_validate({**fields, "value": Decimal("1.5000000")}).value == Decimal("1.5")
        with pytest.raises(ValidationError):
            Messwert.model_validate({**fields, "value": Decimal("1.0000001")})
        with pytest.raises(ValidationError):
            Messwert.model_validate({**fields, "value": Decimal("1000000000000")})
