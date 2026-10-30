"""The four enumerations of the schema. Values are the JSON strings, lower case and case-sensitive."""

from collections.abc import Callable
from enum import StrEnum
from typing import Any, TypeVar

_E = TypeVar("_E", bound=StrEnum)


def enum_from_str(enum: type[_E]) -> Callable[[Any], _E]:
    """Before-validator for an enum field: turn a ``str`` into the member; refuse everything else.

    The models are strict (``strict=True``), and strict mode refuses the plain string ``"wahr"`` for an enum field
    when validating a Python ``dict`` such as ``json.loads`` output. Converting here keeps that input working,
    while ``bytes`` and numbers (which lax enum conversion would accept) stay rejected. Matching is exact:
    ``"Wahr"`` is not ``"wahr"``.
    """

    def convert(value: Any) -> _E:
        if not isinstance(value, str):
            raise ValueError(f"must be a string, not {type(value).__name__}")
        try:
            return enum(value)
        except ValueError:
            raise ValueError(f"{value!r} is not one of {[member.value for member in enum]}") from None

    return convert


class Status(StrEnum):
    """Quality of a measured value."""

    WAHR = "wahr"
    """Confirmed value; its herkunft is always ``fremd``."""
    VORLAEUFIG = "vorlaeufig"
    """Provisional value, may still be replaced."""
    ERSATZWERT = "ersatzwert"
    """Substitute value that was calculated instead of measured."""
    FEHLEND = "fehlend"
    """No value; ``value`` and ``herkunft`` are ``None``."""


class Herkunft(StrEnum):
    """Where a value comes from."""

    FREMD = "fremd"
    """Not formed by the meter data quality agent, e.g. delivered from the source system."""
    MQA = "mqa"
    """Formed by the meter data quality agent (MQA, "Messdatenqualitätsagent") in an earlier run."""


class Profiltyp(StrEnum):
    """Load profile type of the market location."""

    HAUSHALT = "haushalt"
    """Household."""
    GEWERBE = "gewerbe"
    """Commerce."""
    INDUSTRIE = "industrie"
    """Industry."""
    UNBEKANNT = "unbekannt"
    """Unknown."""


class Richtung(StrEnum):
    """Energy direction."""

    BEZUG = "bezug"
    """Consumption (energy taken from the grid)."""
    EINSPEISUNG = "einspeisung"
    """Feed-in (energy delivered to the grid)."""
