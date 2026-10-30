"""Polars table schema of the canonical meter-reading schema (extra ``polars``).

The table form is what Python consumers compute with: seven columns in fixed order, **without** ``resolution``
(consumers only know the 15-minute raster; the column is added when another raster is allowed). Install it with
``pip install mdq-models[polars]``. ``mdq_models`` itself never imports polars.

The names below cover everything a consumer needs from one place: column names, dtypes, the enum value tuples, the
enums and the MaLo helpers (re-exported from :mod:`mdq_models`).

Timestamps are ``Europe/Berlin`` in the table on purpose: consumers do local calendar logic (weekdays, days).
UTC is only the wire format of :class:`~mdq_models.Messwert`; :func:`to_frame` and :func:`from_frame` convert.
"""

from __future__ import annotations

import math
from collections.abc import Iterable
from decimal import ROUND_HALF_EVEN, Context, Decimal, InvalidOperation, localcontext
from typing import Any

try:
    import polars as pl
except ImportError as exc:
    raise ImportError(
        "mdq_models.polars needs the optional dependency polars; install it with: pip install 'mdq-models[polars]'"
    ) from exc

from mdq_models.enums import Herkunft, Profiltyp, Richtung, Status
from mdq_models.malo import herkunft_passt_zu_status, is_valid_malo_id, malo_check_digit
from mdq_models.messwert import Messwert
from mdq_models.scalars import canonical_decimal

__all__ = [
    "BERLIN_TZ",
    "CANONICAL_COLUMNS",
    "CANONICAL_DTYPES",
    "COL_HERKUNFT",
    "COL_MALO",
    "COL_PROFILTYP",
    "COL_RICHTUNG",
    "COL_STATUS",
    "COL_TIMESTAMP",
    "COL_VALUE",
    "HERKUNFT_VALUES",
    "MALO_ID_LENGTH",
    "MAX_FLOAT_VALUE",
    "PROFILTYP_VALUES",
    "RESOLUTION_MINUTES",
    "RICHTUNG_VALUES",
    "STATUS_VALUES",
    "TIMESTAMP_DTYPE",
    "Herkunft",
    "Profiltyp",
    "Richtung",
    "Status",
    "from_frame",
    "herkunft_passt_zu_status",
    "is_valid_malo_id",
    "malo_check_digit",
    "to_frame",
]

BERLIN_TZ = "Europe/Berlin"
"""Time zone of all timestamps in the table."""
RESOLUTION_MINUTES = 15
"""Raster of the table in minutes. There is only this raster, which is why the table has no ``resolution`` column."""
MALO_ID_LENGTH = 11
"""Length of a MaLo id including the check digit."""

COL_MALO = "malo"
COL_TIMESTAMP = "timestamp"
COL_VALUE = "value"
COL_STATUS = "status"
COL_HERKUNFT = "herkunft"
COL_PROFILTYP = "profiltyp"
COL_RICHTUNG = "richtung"

CANONICAL_COLUMNS: tuple[str, ...] = (
    COL_MALO,
    COL_TIMESTAMP,
    COL_VALUE,
    COL_STATUS,
    COL_HERKUNFT,
    COL_PROFILTYP,
    COL_RICHTUNG,
)
"""Column names in the canonical order. ``resolution`` is left out on purpose (see the module docstring)."""

TIMESTAMP_DTYPE = pl.Datetime("us", BERLIN_TZ)
"""Dtype of the timestamp column: time zone aware, microseconds, local time of Germany."""

CANONICAL_DTYPES: dict[str, pl.DataType | type[pl.DataType]] = {
    COL_MALO: pl.String,
    COL_TIMESTAMP: TIMESTAMP_DTYPE,
    COL_VALUE: pl.Float64,
    COL_STATUS: pl.String,
    COL_HERKUNFT: pl.String,
    COL_PROFILTYP: pl.String,
    COL_RICHTUNG: pl.String,
}
"""Dtype per column. ``value`` is Float64 (missing = null); everything but the timestamp is a string."""

STATUS_VALUES: tuple[str, ...] = tuple(member.value for member in Status)
HERKUNFT_VALUES: tuple[str, ...] = tuple(member.value for member in Herkunft)
PROFILTYP_VALUES: tuple[str, ...] = tuple(member.value for member in Profiltyp)
RICHTUNG_VALUES: tuple[str, ...] = tuple(member.value for member in Richtung)

MAX_FLOAT_VALUE = Decimal(10**9)
"""``to_frame`` accepts only ``|value| < MAX_FLOAT_VALUE``. Why: Float64 keeps a decimal exactly only while it has at
most 15 significant digits; with at most 6 decimals, anything below 10^9 has at most 15."""

_SIX_PLACES = Decimal("0.000001")
_FLOAT_BOUND = 10.0**9


def to_frame(rows: Iterable[Messwert]) -> pl.DataFrame:
    """Convert ``Messwert`` rows into the table form (seven columns, canonical dtypes).

    The timestamp is converted from UTC to ``Europe/Berlin`` and ``resolution`` is dropped (it is always PT15M).

    Raises:
        ValueError: if a value has ``|value| >= 10^9``. Float64 could not hold it exactly, and nothing is rounded
            silently.
    """
    rows = list(rows)  # an iterator would be used up by the check below and leave the table empty
    for row in rows:
        if row.value is not None and row.value.copy_abs() >= MAX_FLOAT_VALUE:
            raise ValueError(f"{row.malo} {row.timestamp}: |value| {row.value} is not below 10^9, Float64 is inexact")
    data: dict[str, Any] = {
        COL_MALO: [row.malo for row in rows],
        COL_TIMESTAMP: pl.Series(COL_TIMESTAMP, [row.timestamp for row in rows], dtype=pl.Datetime("us", "UTC")),
        COL_VALUE: [None if row.value is None else float(row.value) for row in rows],
        COL_STATUS: [row.status.value for row in rows],
        COL_HERKUNFT: [None if row.herkunft is None else row.herkunft.value for row in rows],
        COL_PROFILTYP: [row.profiltyp.value for row in rows],
        COL_RICHTUNG: [row.richtung.value for row in rows],
    }
    frame = pl.DataFrame(data, schema_overrides=CANONICAL_DTYPES, schema=list(CANONICAL_COLUMNS))
    return frame.with_columns(pl.col(COL_TIMESTAMP).dt.convert_time_zone(BERLIN_TZ))


def _float_to_decimal(number: float) -> Decimal:
    """Convert a float to a canonical ``Decimal``; ``|number| >= 10^9`` (and inf) is refused.

    The bound is the same as in :func:`to_frame`: above it a Float64 cannot be trusted to 6 decimals, so reading it
    back would invent digits.

    Shortest representation first, not ``Decimal(number)``: that shows the binary expansion
    (``1234567890123.45`` would become ``1234567890123.449951``). Below 10^9 the shortest representation has at most
    6 decimals, so the half-even rounding never decides a tie; it only cleans up values from other producers.
    """
    if not math.isfinite(number):
        raise ValueError(f"{number!r} is not a finite number")
    if abs(number) >= _FLOAT_BOUND:  # a float comparison: no decimal context involved
        raise ValueError(f"|value| {number!r} is not below 10^9, a Float64 cannot be trusted to 6 decimals")
    # An explicit context, so the result does not depend on the caller's precision or traps (a precision of 5 would
    # round 999999999.999999 to 1.0000E+9). 40 digits are plenty: below 10^9 the number has at most 9 + 6 digits.
    with localcontext(Context(prec=40, rounding=ROUND_HALF_EVEN, traps=[InvalidOperation])):
        quantized = Decimal(repr(number)).quantize(_SIX_PLACES, rounding=ROUND_HALF_EVEN)
    return Decimal(canonical_decimal(quantized))


def _check_schema(df: pl.DataFrame) -> None:
    """Strict like the JSON contract: no unknown columns, exactly the canonical dtypes (``Null`` allowed if all null).

    ``cast`` would turn a ``Decimal(20, 6)`` into a float (losing digits), a string ``"1.5"`` into 1.5 and a boolean
    into 1.0 without a word, so the dtypes are compared instead. The timestamp may be in any time zone, but must be
    time zone aware with microsecond precision.
    """
    missing = [name for name in CANONICAL_COLUMNS if name not in df.columns]
    if missing:
        raise ValueError(f"missing columns: {missing}")
    extra = [name for name in df.columns if name not in CANONICAL_DTYPES]
    if extra:
        raise ValueError(
            f"unexpected columns: {extra}; the table form has exactly {list(CANONICAL_COLUMNS)} "
            "(no resolution column, it is always PT15M)"
        )
    for name, actual in df.schema.items():
        expected = CANONICAL_DTYPES[name]
        if actual == pl.Null:
            continue
        if name == COL_TIMESTAMP:
            ok = isinstance(actual, pl.Datetime) and actual.time_zone is not None and actual.time_unit == "us"
            expected = pl.Datetime("us", "<any time zone>")
        else:
            ok = actual == expected
        if not ok:
            raise ValueError(f"column {name!r} has dtype {actual}, expected {expected}")


def from_frame(df: pl.DataFrame) -> list[Messwert]:
    """Convert a table in the canonical form back into validated ``Messwert`` rows.

    The frame must be a ``DataFrame`` (not a ``LazyFrame``: ``collect()`` it first), have exactly the seven canonical
    columns with the canonical dtypes (the timestamp may be in any time zone; it is converted to UTC). Extra columns
    are rejected, a ``resolution`` column too: the table has no resolution, it is always PT15M and is set here. Why
    strict: like the JSON contract, which rejects unknown keys, a typo or a new field must fail loudly.

    A float ``value`` is converted through its shortest representation and quantized to 6 decimals (half-even). The
    bound of :func:`to_frame` applies here too: ``|value| >= 10^9`` raises, because a Float64 that large cannot be
    trusted to 6 decimals.

    Raises:
        TypeError: if ``df`` is not a ``DataFrame``.
        ValueError: if a column is missing, unexpected or has the wrong dtype, the timestamp is naive, or a float is
            not below 10^9 in absolute value.
        pydantic.ValidationError: if a row violates the contract (bad MaLo, status/herkunft mismatch, ...).
    """
    if not isinstance(df, pl.DataFrame):
        raise TypeError(f"expected a polars DataFrame, got {type(df).__name__}; collect() a LazyFrame first")
    _check_schema(df)
    # Only an all-null (Null dtype) column needs a cast; every other dtype was checked to be the canonical one.
    timestamp = pl.col(COL_TIMESTAMP)
    if df.schema[COL_TIMESTAMP] == pl.Null:
        timestamp = timestamp.cast(pl.Datetime("us", "UTC"))
    table = df.select(
        pl.col(COL_MALO).cast(pl.String),
        timestamp.dt.convert_time_zone("UTC"),
        pl.col(COL_VALUE).cast(pl.Float64),
        *(pl.col(name).cast(pl.String) for name in CANONICAL_COLUMNS[3:]),
    )
    return [
        Messwert(
            malo=row[COL_MALO],
            timestamp=row[COL_TIMESTAMP],
            resolution="PT15M",
            value=None if row[COL_VALUE] is None else _float_to_decimal(row[COL_VALUE]),
            status=row[COL_STATUS],
            herkunft=row[COL_HERKUNFT],
            profiltyp=row[COL_PROFILTYP],
            richtung=row[COL_RICHTUNG],
        )
        for row in table.iter_rows(named=True)
    ]
