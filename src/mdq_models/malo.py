"""MaLo ids and the check digit (BDEW algorithm), plus the herkunft/status rule."""

import re
from typing import Annotated

from pydantic import AfterValidator, StrictStr

from mdq_models.enums import Herkunft, Status

# re.ASCII: without it \d and friends would accept other scripts' digits; fullmatch: `$` would match before a
# trailing newline. Go uses the same expression.
_MALO = re.compile(r"[1-9][0-9]{10}", re.ASCII)
_TEN_DIGITS = re.compile(r"[0-9]{10}", re.ASCII)


def malo_check_digit(first_ten: str) -> int:
    """Check digit for the first ten digits of a MaLo id.

    The digits at odd positions (1st, 3rd, ...) count once, those at even positions twice;
    the check digit is ``(10 - total % 10) % 10``.

    Raises:
        ValueError: ``first_ten`` is not exactly ten ASCII digits (a leading 0 is fine here; the id rule in
            :func:`is_valid_malo_id` is stricter).
    """
    if not _TEN_DIGITS.fullmatch(first_ten):
        raise ValueError(f"{first_ten!r} is not ten ASCII digits")
    total = sum(int(digit) * (2 if index % 2 else 1) for index, digit in enumerate(first_ten))
    return (10 - total % 10) % 10


def is_valid_malo_id(value: str) -> bool:
    """Whether ``value`` is 11 ASCII digits, does not start with 0 and ends in the correct check digit."""
    return _MALO.fullmatch(value) is not None and malo_check_digit(value[:10]) == int(value[10])


def _check_malo(value: str) -> str:
    if not _MALO.fullmatch(value):
        raise ValueError(f"{value!r} is not 11 ASCII digits without a leading 0")
    if not is_valid_malo_id(value):
        raise ValueError(f"{value!r} has a wrong check digit")
    return value


MaLo = Annotated[StrictStr, AfterValidator(_check_malo)]
"""A market location id. Strict string: a JSON number is rejected rather than converted."""


def herkunft_passt_zu_status(status: Status | str, herkunft: Herkunft | str | None) -> bool:
    """Whether the combination is allowed.

    ``fehlend`` has no herkunft, ``wahr`` needs ``fremd``, ``vorlaeufig`` and ``ersatzwert`` may have either.
    Only ``None`` means "no herkunft"; the empty string is an unknown value, not "unset" (Go takes a pointer
    for the same reason). An unknown status or herkunft, including a wrong case (``"Wahr"``), is never allowed:
    a rule that says "anything else goes" would let a typo through.
    """
    try:
        known_status = Status(status)
        known_herkunft = None if herkunft is None else Herkunft(herkunft)
    except ValueError:
        return False
    if known_status == Status.FEHLEND:
        return known_herkunft is None
    if known_herkunft is None:
        return False
    return known_status != Status.WAHR or known_herkunft == Herkunft.FREMD
