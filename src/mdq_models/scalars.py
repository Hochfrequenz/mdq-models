"""Scalar types with the contract's syntax rules: canonical decimals and UTC timestamps."""

import re
from datetime import UTC, datetime
from decimal import Decimal
from typing import Annotated, Any

from pydantic import PlainSerializer, PlainValidator

MAX_DECIMAL_PLACES = 6
"""At most this many digits after the point, counted after trailing zeros are removed."""

MAX_INTEGER_DIGITS = 12
"""At most this many digits before the point (|value| < 10^12). Why: a common storage type for such values is
a numeric(18,6) database column (12 digits before, 6 after the point), so a larger value would pass validation and
only fail on write. It also bounds the memory a Decimal built in code like ``1E+999999999`` could use. Go checks the
same."""

# [0-9], not \d, and fullmatch, not `$`: Python's `$` matches before a trailing newline and \d matches other
# scripts' digits. Go uses the same expression.
_CANONICAL_DECIMAL = re.compile(r"-?(0|[1-9][0-9]{0,11})(\.[0-9]{0,5}[1-9])?", re.ASCII)
_TIMESTAMP = re.compile(r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})Z", re.ASCII)

MIN_YEAR = 1900
MAX_YEAR = 9999


def _decimal_error(value: object, rule: str) -> ValueError:
    return ValueError(f"{value!r}: {rule}")


def canonical_decimal(value: Decimal) -> str:
    """Write ``value`` in the shortest canonical form, or raise ``ValueError`` if it is not allowed.

    Not ``Decimal.normalize()``: that rounds to the context precision (28 digits by default) and would silently
    change long values. The text is built with ``format(value, "f")`` and stripped as a string instead.
    """
    if not value.is_finite():  # NaN, sNaN and the infinities have no decimal spelling
        raise _decimal_error(value, "must be a finite number")
    _sign, digits, exponent = value.as_tuple()
    assert isinstance(exponent, int)  # finite
    # Count places and integer digits without formatting, so 1E-999999999 is rejected before any string is built.
    # Trailing zero digits are dropped first: Decimal("1.5000000") has 7 places but is 1.5.
    kept = "".join(map(str, digits)).rstrip("0")
    if not kept:  # zero in any spelling (0, 0.0, 0E-10, -0)
        return "0"
    exponent += len(digits) - len(kept)
    if -exponent > MAX_DECIMAL_PLACES:
        raise _decimal_error(value, f"has more than {MAX_DECIMAL_PLACES} decimal places (it is rejected, not rounded)")
    if len(kept) + exponent > MAX_INTEGER_DIGITS:
        raise _decimal_error(value, f"has more than {MAX_INTEGER_DIGITS} integer digits")
    text = format(value, "f")
    if "." in text:
        text = text.rstrip("0").removesuffix(".")
    return text


def _to_decimal(value: Any) -> Decimal:
    """Accept a canonical string (JSON) or a ``Decimal`` (code, normalised); nothing else.

    No ``float`` or ``int``: a float is already inexact, and a JSON number would let ``1.0`` and ``1`` both through.
    """
    if isinstance(value, Decimal):
        return Decimal(canonical_decimal(value))
    if isinstance(value, str):
        if not _CANONICAL_DECIMAL.fullmatch(value) or value == "-0":
            raise _decimal_error(
                value, "is not a decimal in shortest canonical form with at most 12 integer and 6 decimal digits"
            )
        return Decimal(value)
    raise ValueError(f"{value!r}: must be a canonical decimal string or a Decimal, not {type(value).__name__}")


CanonicalDecimal = Annotated[
    Decimal,
    PlainValidator(_to_decimal),
    PlainSerializer(canonical_decimal, return_type=str, when_used="json"),
]
"""A decimal number with at most 12 integer and 6 decimal digits.
JSON carries the shortest canonical string (``"1.5"``)."""


def _utc_timestamp(value: Any, *, quarter_hour: bool) -> datetime:
    if isinstance(value, str):
        match = _TIMESTAMP.fullmatch(value)
        if not match:
            raise ValueError(f"{value!r}: must look like 2026-01-15T10:15:00Z (upper-case T and Z, whole seconds)")
        try:  # a real calendar parse: rejects 02-30, hour 24, second 60 and 1900-02-29
            year, month, day, hour, minute, second = (int(part) for part in match.groups())
            parsed = datetime(year, month, day, hour, minute, second, tzinfo=UTC)
        except ValueError as exc:
            raise ValueError(f"{value!r}: {exc}") from exc
    elif isinstance(value, datetime):
        if value.tzinfo is None or value.utcoffset() is None:
            raise ValueError(f"{value!r}: naive datetimes are not allowed, attach a time zone")
        try:
            parsed = value.astimezone(UTC)
        except OverflowError as exc:  # the instant lies outside years 1 to 9999 in UTC
            raise ValueError(f"{value!r}: outside the years {MIN_YEAR} to {MAX_YEAR} in UTC") from exc
        # Checked after the conversion, not before: an offset with a sub-second part (timedelta(microseconds=1))
        # turns a whole-second local time into a UTC time with microseconds, which the JSON form cannot carry,
        # so the output would name a different instant.
        if parsed.microsecond:
            raise ValueError(f"{value!r}: only whole seconds are allowed (in UTC)")
    else:
        raise ValueError(f"{value!r}: must be an RFC 3339 string or an aware datetime")
    if not MIN_YEAR <= parsed.year <= MAX_YEAR:
        raise ValueError(f"{value!r}: outside the years {MIN_YEAR} to {MAX_YEAR} in UTC")
    # The raster is checked on the UTC value, so an input with another offset is judged by the instant it means.
    if quarter_hour and (parsed.minute % 15 or parsed.second):
        raise ValueError(f"{value!r}: must be the start of a quarter hour (minute 0, 15, 30 or 45, second 0)")
    return parsed


def _format_utc(value: datetime) -> str:
    """Always ``Z``; never ``+00:00``, so the wire form has exactly one spelling."""
    return value.strftime("%Y-%m-%dT%H:%M:%SZ")


QuarterHourStart = Annotated[
    datetime,
    PlainValidator(lambda v: _utc_timestamp(v, quarter_hour=True)),
    PlainSerializer(_format_utc, return_type=str, when_used="json"),
]
"""UTC instant on the 15-minute raster: the start of a quarter hour. Aware datetimes are converted to UTC."""

UtcSecond = Annotated[
    datetime,
    PlainValidator(lambda v: _utc_timestamp(v, quarter_hour=False)),
    PlainSerializer(_format_utc, return_type=str, when_used="json"),
]
"""UTC instant with whole seconds and no raster. Aware datetimes are converted to UTC."""
