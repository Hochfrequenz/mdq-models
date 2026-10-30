"""A meter reading (register value) at a point in time."""

from pydantic import Field

from mdq_models._base import Model
from mdq_models.malo import MaLo
from mdq_models.scalars import CanonicalDecimal, UtcSecond


class Zaehlerstand(Model):
    """A meter reading of one market location. Field order is the key order of the JSON output."""

    malo: MaLo = Field(description="Market location id: 11 ASCII digits, no leading 0, valid check digit.")
    timestamp: UtcSecond = Field(
        description="Point in time of the reading, UTC, whole seconds, no raster. JSON uses 'Z'."
    )
    value: CanonicalDecimal = Field(description="Register value with at most 6 decimal places; never null.")
