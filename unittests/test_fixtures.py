"""Contract fixtures that need no model code: shape, reasons and shared cases.

Must stay consistent with fixtures_test.go.
"""

import json
import re
from datetime import datetime
from pathlib import Path
from typing import Any

import pytest

TESTDATA = Path(__file__).resolve().parent.parent / "testdata"
KINDS = ("messwert", "zaehlerstand")
KEYS = {
    "messwert": ["malo", "timestamp", "resolution", "value", "status", "herkunft", "profiltyp", "richtung"],
    "zaehlerstand": ["malo", "timestamp", "value"],
}
MALO_RE = re.compile(r"^[1-9][0-9]{10}$", re.ASCII)
TIMESTAMP_RE = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$", re.ASCII)
# plan 0.3: shortest canonical form, at most 6 decimal places, no negative zero
DECIMAL_RE = re.compile(r"-?(0|[1-9][0-9]{0,11})(\.[0-9]{0,5}[1-9])?", re.ASCII)
ENUMS = {
    "status": {"wahr", "vorlaeufig", "ersatzwert", "fehlend"},
    "herkunft": {"fremd", "mqa"},
    "profiltyp": {"haushalt", "gewerbe", "industrie", "unbekannt"},
    "richtung": {"bezug", "einspeisung"},
}
# Valid fixtures that deliberately do not use the contract key order (parsing accepts any order).
REORDERED = {"messwert/key_order_reversed.json", "zaehlerstand/key_order_reversed.json"}
# The only fixtures that contain a duplicate key at some level; duplicate keys are rejected on purpose.
WITH_DUPLICATES = {"messwert/duplicate_key.json", "zaehlerstand/duplicate_key.json"}


class DuplicateKeyError(ValueError):
    """A JSON object contains the same key twice."""


def _strict_pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    keys = [k for k, _ in pairs]
    if len(keys) != len(set(keys)):
        raise DuplicateKeyError(sorted(k for k in set(keys) if keys.count(k) > 1))
    return dict(pairs)


def has_duplicate_keys(raw: bytes) -> bool:
    """True if any object anywhere in the document repeats a key."""
    try:
        json.loads(raw, object_pairs_hook=_strict_pairs)
    except DuplicateKeyError:
        return True
    return False


def reference_errors(kind: str, data: Any) -> list[str]:
    """Reference validator for plan 0.1-0.5, deliberately independent of the models. Empty list means valid."""
    if not isinstance(data, dict):
        return ["top level is not an object"]
    if set(data) != set(KEYS[kind]):
        return [f"key set {sorted(data)}"]
    errors: list[str] = []
    malo = data["malo"]
    if not isinstance(malo, str) or not is_valid_malo(malo):
        errors.append("malo")
    errors.extend(_timestamp_errors(kind, data["timestamp"]))
    value = data["value"]
    if value is None:
        if kind == "zaehlerstand" or data["status"] != "fehlend":
            errors.append("value is null")
    elif not isinstance(value, str) or not DECIMAL_RE.fullmatch(value) or value == "-0":
        errors.append("value is not a canonical decimal with at most 6 decimal places")
    elif kind == "messwert" and data["status"] == "fehlend":
        errors.append("value set for fehlend")
    if kind == "messwert":
        errors.extend(_messwert_errors(data))
    return errors


def _timestamp_errors(kind: str, ts: Any) -> list[str]:
    if not isinstance(ts, str) or not TIMESTAMP_RE.fullmatch(ts):
        return ["timestamp shape"]
    try:
        parsed = datetime.strptime(ts, "%Y-%m-%dT%H:%M:%SZ")
    except ValueError:
        return ["timestamp is not a calendar time"]
    errors = [] if 1900 <= parsed.year <= 9999 else ["timestamp year"]
    if kind == "messwert" and (parsed.minute % 15 != 0 or parsed.second != 0):
        errors.append("timestamp is off the 15-minute raster")
    return errors


def _messwert_errors(data: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    if data["resolution"] != "PT15M":
        errors.append("resolution")
    for key in ("status", "profiltyp", "richtung"):
        if not isinstance(data[key], str) or data[key] not in ENUMS[key]:
            errors.append(key)
    herkunft = data["herkunft"]
    if herkunft is not None and (not isinstance(herkunft, str) or herkunft not in ENUMS["herkunft"]):
        errors.append("herkunft")
    status = data["status"]
    if status == "fehlend" and herkunft is not None:
        errors.append("herkunft set for fehlend")
    elif status in ENUMS["status"] - {"fehlend"} and herkunft is None:
        errors.append("herkunft null")  # also covers wahr; one violation per fixture
    elif status == "wahr" and herkunft in ENUMS["herkunft"] and herkunft != "fremd":
        errors.append("wahr requires fremd")
    return errors


def _files(validity: str) -> list[Path]:
    files: list[Path] = []
    for kind in KINDS:
        found = sorted((TESTDATA / kind / validity).glob("*.json"))
        assert found, f"no fixtures in {kind}/{validity}"
        files.extend(found)
    return files


# Invalid fixtures about the text encoding (BOM, UTF-16, invalid UTF-8): not JSON by design, so the
# tests that decode fixtures as JSON skip them.
NOT_JSON = {
    f"{kind}/{file}"
    for kind in ("messwert", "zaehlerstand")
    for file in ("utf8_bom.json", "utf16_encoded.json", "invalid_utf8_bytes.json")
}


def _json_files(validity: str) -> list[Path]:
    return [p for p in _files(validity) if _name(p) not in NOT_JSON]


def _name(path: Path) -> str:
    """``messwert/x.json``, the key used in invalid_reasons.json."""
    return f"{path.parent.parent.name}/{path.name}"


def check_digit(first_ten: str) -> int:
    """Minimal BDEW check digit; raises ValueError on input that is not exactly ten ASCII digits."""
    if not re.fullmatch(r"[0-9]{10}", first_ten, re.ASCII):
        raise ValueError(first_ten)
    total = sum(int(c) * (2 if i % 2 else 1) for i, c in enumerate(first_ten))
    return (10 - total % 10) % 10


def is_valid_malo(value: str) -> bool:
    return bool(MALO_RE.fullmatch(value)) and check_digit(value[:10]) == int(value[10])


def _load(name: str) -> Any:
    """Load a shared file, failing on duplicate keys (a plain json.loads would silently keep the last one)."""
    return json.loads((TESTDATA / name).read_bytes(), object_pairs_hook=_strict_pairs)


@pytest.mark.parametrize("path", _files("valid"), ids=_name)
def test_valid_fixture_is_a_json_object(path: Path) -> None:
    assert isinstance(json.loads(path.read_bytes()), dict)


@pytest.mark.parametrize("path", _json_files("invalid"), ids=_name)
def test_invalid_fixture_is_json(path: Path) -> None:
    """Invalid fixtures need not be objects (``[]``, ``null``, ``"x"``), but they must be parseable JSON."""
    json.loads(path.read_bytes())


def test_duplicate_keys_occur_exactly_in_the_duplicate_key_fixtures() -> None:
    found = {_name(p) for p in _files("valid") + _json_files("invalid") if has_duplicate_keys(p.read_bytes())}
    assert found == WITH_DUPLICATES
    for name in found:
        kind, file = name.split("/")
        assert (TESTDATA / kind / "invalid" / file).exists(), name


def test_duplicate_keys_in_a_document_are_detected() -> None:
    assert has_duplicate_keys(b'{"a": 1, "a": 2}')
    assert has_duplicate_keys(b'{"a": {"b": 1, "b": 2}}')
    assert has_duplicate_keys(b'[{"a": 1, "a": 2}]')
    assert not has_duplicate_keys(b'{"a": {"b": 1}, "b": 2}')


def test_invalid_reasons_cover_exactly_the_invalid_fixtures() -> None:
    reasons: dict[str, str] = _load("invalid_reasons.json")
    assert set(reasons) == {_name(p) for p in _files("invalid")}
    for name, reason in reasons.items():
        assert reason.strip(), name
        assert "\n" not in reason, name


@pytest.mark.parametrize("path", _files("valid"), ids=_name)
def test_valid_fixture_follows_the_contract_shape(path: Path) -> None:
    kind = path.parent.parent.name
    data = json.loads(path.read_bytes())
    if _name(path) in REORDERED:
        assert list(data) != KEYS[kind]
        assert sorted(data) == sorted(KEYS[kind])
    else:
        assert list(data) == KEYS[kind]
    assert reference_errors(kind, data) == []


def _drop_repeated_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        result.setdefault(key, value)  # keeps the first occurrence, drops the later ones
    return result


@pytest.mark.parametrize("path", _json_files("invalid"), ids=_name)
def test_invalid_fixture_violates_exactly_one_rule(path: Path) -> None:
    """Catches fixtures that are not invalid, or that break several rules at once."""
    raw = path.read_bytes()
    kind = path.parent.parent.name
    if has_duplicate_keys(raw):
        # only the repeated key makes it invalid: valid once the later occurrence is dropped
        assert reference_errors(kind, json.loads(raw, object_pairs_hook=_drop_repeated_keys)) == []
        return
    doc = json.loads(raw)
    if not isinstance(doc, dict):
        return
    assert len(reference_errors(kind, doc)) == 1, reference_errors(kind, doc)


def test_non_object_top_levels_are_rejected_by_the_reference_validator() -> None:
    docs: list[Any] = [[], None, "x"]
    for doc in docs:
        assert reference_errors("messwert", doc) != []


@pytest.mark.parametrize(
    ("data", "ok"),
    [
        ({"malo": "68283561554", "timestamp": "2026-01-15T10:17:23Z", "value": "0.000001"}, True),
        ({"malo": "68283561554", "timestamp": "2026-01-15T10:17:23Z", "value": "0.0000001"}, False),
        ({"malo": "68283561554", "timestamp": "2026-01-15T10:17:23Z", "value": "1.10"}, False),
        ({"malo": "68283561554", "timestamp": "1900-02-29T00:00:00Z", "value": "1"}, False),
        ({"malo": "68283561554", "timestamp": "2000-02-29T00:00:00Z", "value": "1"}, True),
        ({"malo": "68283561554", "timestamp": "2026-01-15T10:17:23Z", "value": "-0"}, False),
    ],
)
def test_reference_validator_sanity(data: dict[str, Any], ok: bool) -> None:
    assert (reference_errors("zaehlerstand", data) == []) is ok


def test_malo_ids_agree_with_check_digit() -> None:
    cases = _load("malo_ids.json")
    assert "68283561554" in cases["valid"]
    for malo in cases["valid"]:
        assert is_valid_malo(malo), malo
    assert cases["invalid"]
    for case in cases["invalid"]:
        assert case["reason"]
        assert not is_valid_malo(case["id"]), case
    for case in cases["check_digits"]:
        assert check_digit(case["first_ten"]) == case["check_digit"], case
    assert cases["check_digit_invalid_input"]
    for case in cases["check_digit_invalid_input"]:
        with pytest.raises(ValueError):
            check_digit(case["first_ten"])


def test_herkunft_status_is_complete_and_follows_the_rule() -> None:
    cases = _load("herkunft_status.json")
    assert len(cases) >= 12
    pairs = {(c["status"], c["herkunft"]) for c in cases}
    assert len(pairs) == len(cases)
    for status in ENUMS["status"]:
        for herkunft in (None, *ENUMS["herkunft"]):
            assert (status, herkunft) in pairs
    for case in cases:
        status, herkunft = case["status"], case["herkunft"]
        if status not in ENUMS["status"] or (herkunft is not None and herkunft not in ENUMS["herkunft"]):
            expected = False  # unknown values, and "" is an unknown herkunft rather than unset
        elif status == "fehlend":
            expected = herkunft is None
        elif status == "wahr":
            expected = herkunft == "fremd"
        else:
            expected = herkunft is not None
        assert case["ok"] is expected, case


def test_proto_enums_shape() -> None:
    enums = _load("proto_enums.json")
    assert set(enums) == {"Status", "Herkunft", "Profiltyp", "Richtung"}
    for name, enum in enums.items():
        prefix = name.upper()
        assert enum["unspecified"] == f"{prefix}_UNSPECIFIED"
        assert enum["values"]
        for number, value in enumerate(enum["values"], start=1):
            assert value["number"] == number
            assert value["json"]
            assert value["name"] == f"{prefix}_{value['json'].upper()}"
