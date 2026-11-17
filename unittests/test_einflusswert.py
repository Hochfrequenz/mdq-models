"""Tests of the Einflusswert model (an influence value such as temperature for one market location)."""

from datetime import datetime, timedelta, timezone
from typing import Any

import pytest
from pydantic import ValidationError

from mdq_models import Einflusswert


def _valid(**overrides: Any) -> dict[str, Any]:
    data: dict[str, Any] = {
        "malo": "68283561554",
        "timestamp": "2026-01-15T10:00:00Z",
        "resolution": "PT1H",
        "groesse": "temperatur",
        "value": "-3.5",
        "quelle": "dwd:01993",
    }
    data.update(overrides)
    return data


def test_json_key_order_and_bytes() -> None:
    assert Einflusswert.model_validate(_valid()).model_dump_json() == (
        '{"malo":"68283561554","timestamp":"2026-01-15T10:00:00Z","resolution":"PT1H",'
        '"groesse":"temperatur","value":"-3.5","quelle":"dwd:01993"}'
    )


def test_missing_value_is_written_as_null() -> None:
    assert '"value":null' in Einflusswert.model_validate(_valid(value=None)).model_dump_json()
    data = _valid()
    del data["value"]
    with pytest.raises(ValidationError):
        Einflusswert.model_validate(data)


def test_value_zero_stays_zero() -> None:
    assert '"value":"0"' in Einflusswert.model_validate(_valid(value="0")).model_dump_json()


def test_pt1h_requires_full_hour() -> None:
    with pytest.raises(ValidationError):
        Einflusswert.model_validate(_valid(timestamp="2026-01-15T10:15:00Z"))
    Einflusswert.model_validate(_valid(timestamp="2026-01-15T10:15:00Z", resolution="PT15M"))
    with pytest.raises(ValidationError):
        Einflusswert.model_validate(_valid(timestamp="2026-01-15T10:07:00Z", resolution="PT15M"))


def test_raster_is_checked_on_the_utc_instant() -> None:
    ts = datetime(2026, 1, 15, 16, 0, tzinfo=timezone(timedelta(hours=5, minutes=30)))
    with pytest.raises(ValidationError):
        Einflusswert.model_validate(_valid(timestamp=ts))
    m = Einflusswert.model_validate(_valid(timestamp=ts, resolution="PT15M"))
    assert '"timestamp":"2026-01-15T10:30:00Z"' in m.model_dump_json()


@pytest.mark.parametrize(
    ("ts", "expected"),
    [
        (datetime(2026, 10, 25, 2, 0, tzinfo=timezone(timedelta(hours=2))), "2026-10-25T00:00:00Z"),
        (datetime(2026, 10, 25, 2, 0, tzinfo=timezone(timedelta(hours=1))), "2026-10-25T01:00:00Z"),
        (datetime(2026, 3, 29, 3, 0, tzinfo=timezone(timedelta(hours=2))), "2026-03-29T01:00:00Z"),
    ],
)
def test_dst_hours_with_explicit_offset(ts: datetime, expected: str) -> None:
    assert f'"timestamp":"{expected}"' in Einflusswert.model_validate(_valid(timestamp=ts)).model_dump_json()


@pytest.mark.parametrize("quelle", ["dwd:01993", "a", "x" * 64])
def test_quelle_valid(quelle: str) -> None:
    assert Einflusswert.model_validate(_valid(quelle=quelle)).quelle == quelle


@pytest.mark.parametrize(
    "quelle", ["", "dwd: 01993", "dwd:01993 ", "dwd:München", "x" * 65, "dwd:01993\n", 1993, b"dwd:01993"]
)
def test_quelle_invalid(quelle: Any) -> None:
    with pytest.raises(ValidationError):
        Einflusswert.model_validate(_valid(quelle=quelle))


@pytest.mark.parametrize(
    "override",
    [
        {"groesse": "Temperatur"},
        {"groesse": "cloud_cover"},
        {"resolution": "pt1h"},
        {"groesse": b"temperatur"},
    ],
)
def test_groesse_and_resolution_strings_are_exact(override: dict[str, Any]) -> None:
    with pytest.raises(ValidationError):
        Einflusswert.model_validate(_valid(**override))


def test_unknown_and_duplicate_keys_are_rejected() -> None:
    with pytest.raises(ValidationError):
        Einflusswert.model_validate(_valid(einheit="C"))
    raw = Einflusswert.model_validate(_valid()).model_dump_json()[:-1] + ',"quelle":"other"}'
    with pytest.raises(ValidationError):
        Einflusswert.model_validate_json(raw)


def test_frozen_and_described() -> None:
    m = Einflusswert.model_validate(_valid())
    with pytest.raises(ValidationError):
        m.quelle = "x"  # type: ignore[misc]
    for name, field in Einflusswert.model_fields.items():
        assert field.description, name
