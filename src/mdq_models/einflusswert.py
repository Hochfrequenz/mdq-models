"""One row of an influence series: an influence value (for example temperature) for one market location."""

import re
from typing import Annotated, Self

from pydantic import AfterValidator, BeforeValidator, Field, model_validator

from mdq_models._base import Model
from mdq_models.enums import EinflussResolution, Groesse, enum_from_str
from mdq_models.malo import MaLo
from mdq_models.scalars import CanonicalDecimal, QuarterHourStart

QUELLE_MAX_LENGTH = 64
QUELLE_PATTERN = re.compile(r"[!-~]{1,64}", re.ASCII)


def _check_quelle(value: str) -> str:
    # fullmatch, not match/$: `$` would accept a trailing newline.
    if QUELLE_PATTERN.fullmatch(value) is None:
        raise ValueError(f"quelle must be 1 to {QUELLE_MAX_LENGTH} printable ASCII characters without spaces")
    return value


Quelle = Annotated[str, AfterValidator(_check_quelle)]


class Einflusswert(Model):
    """An influence value of one market location at one point in time.

    Field order is the key order of the JSON output (the Go models write the same order). All six keys are
    required; ``value`` is required but nullable, and always written (``null`` when missing; ``0`` is a value).
    """

    malo: MaLo = Field(description="Market location id: 11 ASCII digits, no leading 0, valid check digit.")
    timestamp: QuarterHourStart = Field(
        description="Start of the interval, UTC; on the quarter-hour raster, and on the full hour for PT1H."
    )
    resolution: Annotated[EinflussResolution, BeforeValidator(enum_from_str(EinflussResolution))] = Field(
        description="Raster of the series: PT15M or PT1H."
    )
    groesse: Annotated[Groesse, BeforeValidator(enum_from_str(Groesse))] = Field(
        description="The measured quantity, for example temperatur."
    )
    value: CanonicalDecimal | None = Field(
        description="Value with at most 6 decimal places, written as a canonical string; None if missing."
    )
    quelle: Quelle = Field(
        description="Source of the value: 1 to 64 printable ASCII characters without spaces, for example dwd:01993."
    )

    @model_validator(mode="after")
    def _check_raster(self) -> Self:
        if self.resolution == EinflussResolution.PT1H and self.timestamp.minute != 0:
            raise ValueError("timestamp must be on the full hour (UTC) for resolution PT1H")
        return self
