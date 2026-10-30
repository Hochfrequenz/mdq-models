"""Hand-written data models of the canonical meter-reading schema.

The contract is the JSON form of these models, pinned by the shared fixtures in ``testdata/`` that
the Go and the Python models both round-trip. Nothing here is generated.

Read JSON with ``Messwert.model_validate_json`` (it rejects duplicate keys, which plain pydantic would
silently resolve) and write it with ``model_dump_json()``.

Limit: only ``model_validate_json`` of ``Messwert`` and ``Zaehlerstand`` rejects duplicate keys.
``TypeAdapter(...).validate_json``, a model that contains one of them and ``__pydantic_validator__`` go straight to
pydantic's parser and keep the last duplicate; call :func:`check_json` on the raw document first. All parse
failures are ``pydantic.ValidationError``.
"""

from mdq_models._base import check_json
from mdq_models._version import version as __version__
from mdq_models.enums import Herkunft, Profiltyp, Richtung, Status
from mdq_models.malo import herkunft_passt_zu_status, is_valid_malo_id, malo_check_digit
from mdq_models.messwert import Messwert
from mdq_models.zaehlerstand import Zaehlerstand

__all__ = [
    "Herkunft",
    "Messwert",
    "Profiltyp",
    "Richtung",
    "Status",
    "Zaehlerstand",
    "__version__",
    "check_json",
    "herkunft_passt_zu_status",
    "is_valid_malo_id",
    "malo_check_digit",
]
