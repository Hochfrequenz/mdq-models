"""One row of the canonical schema: a quarter-hour meter value."""

from typing import Annotated, Literal, Self

from pydantic import BeforeValidator, Field, model_validator

from mdq_models._base import Model
from mdq_models.enums import Herkunft, Profiltyp, Richtung, Status, enum_from_str
from mdq_models.malo import MaLo, herkunft_passt_zu_status
from mdq_models.scalars import CanonicalDecimal, QuarterHourStart


class Messwert(Model):
    """A meter value of one market location for one quarter hour.

    Field order is the key order of the JSON output (the Go models write the same order). All eight keys are
    required; ``value`` and ``herkunft`` are required but nullable, and always written (``null`` when unset).
    """

    malo: MaLo = Field(description="Market location id: 11 ASCII digits, no leading 0, valid check digit.")
    timestamp: QuarterHourStart = Field(
        description="Start of the quarter hour, UTC (00:00 is the first of a day, 23:45 the last). JSON uses 'Z'."
    )
    resolution: Literal["PT15M"] = Field(description="Raster of the series; only PT15M in v1.")
    value: CanonicalDecimal | None = Field(
        description="Energy value with at most 6 decimal places, written as a canonical string; None iff fehlend."
    )
    status: Annotated[Status, BeforeValidator(enum_from_str(Status))] = Field(description="Quality of the value.")
    herkunft: Annotated[Herkunft, BeforeValidator(enum_from_str(Herkunft))] | None = Field(
        description="Origin of the value; None iff fehlend; wahr requires fremd."
    )
    profiltyp: Annotated[Profiltyp, BeforeValidator(enum_from_str(Profiltyp))] = Field(
        description="Load profile type of the market location."
    )
    richtung: Annotated[Richtung, BeforeValidator(enum_from_str(Richtung))] = Field(description="Energy direction.")

    @model_validator(mode="after")
    def _check_missing_and_herkunft(self) -> Self:
        # `fehlend` means there is nothing to report, so a value or an origin would contradict it;
        # every other status needs both, or a consumer could not tell a missing value from a zero.
        missing = self.status == Status.FEHLEND
        if (self.value is None) != missing:
            raise ValueError("value must be None if and only if status is fehlend")
        if not herkunft_passt_zu_status(self.status, self.herkunft):
            raise ValueError(f"herkunft {self.herkunft!r} does not fit status {self.status.value!r}")
        return self
