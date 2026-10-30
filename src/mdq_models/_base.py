"""Base class of the models: strict reading (duplicate and unknown keys are rejected) and frozen instances."""

import json
import re
from typing import Any, NoReturn, Self

from pydantic import BaseModel, ConfigDict, ValidationError
from pydantic_core import InitErrorDetails, PydanticCustomError


class _DuplicateKeyError(ValueError):
    """A JSON object repeats a key (internal: it is turned into a ValidationError)."""


def _reject_duplicates(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    seen: set[str] = set()
    for key, _ in pairs:
        if key in seen:
            raise _DuplicateKeyError(f"duplicate key {key!r}")
        seen.add(key)
    return dict(pairs)


def _reject_constant(name: str) -> NoReturn:
    # json.loads accepts NaN and Infinity by default; they are not JSON.
    raise ValueError(f"{name} is not valid JSON")


MAX_DEPTH = 100
"""The deepest nesting of arrays and objects a document may have. The schema is flat; the limit exists so that the
answer to a hostile document does not depend on the interpreter's recursion limit, which differs between Python
versions (3.13 and later report a stack overflow, 3.11 a recursion error) and between builds."""

# A string (so brackets inside it are skipped, including escaped quotes) or a bracket.
_NESTING = re.compile(r'"(?:[^"\\]|\\.)*"|[\[\]{}]', re.DOTALL)


def _too_deep(text: str) -> bool:
    depth = 0
    for match in _NESTING.finditer(text):
        token = match.group()
        if token in "[{":
            depth += 1
            if depth > MAX_DEPTH:
                return True
        elif token in "]}":
            depth -= 1
    return False


def _fail(title: str, code: str, message: str) -> NoReturn:
    """Raise a ``ValidationError``, the one error type callers (for example web frameworks) already handle.

    The input is left out of the error: a document can be megabytes long.
    """
    detail = InitErrorDetails(type=PydanticCustomError(code, message), loc=(), input=None)
    raise ValidationError.from_exception_data(title, [detail])


def _read(data: str | bytes | bytearray, title: str) -> str:
    """Decode ``data`` as UTF-8 without BOM and check it is JSON without repeated keys; return the text.

    RFC 8259 says JSON is UTF-8 and that parsers must not add a BOM. ``json.loads`` would silently accept
    UTF-16 and UTF-32 and skip a BOM, so a document the Go models reject would pass here; decode strictly instead.
    """
    if isinstance(data, str):
        text = data
    else:
        try:
            text = bytes(data).decode("utf-8")
        except UnicodeDecodeError as exc:
            _fail(title, "not_utf8", f"the document is not UTF-8: {exc.reason}")
    if text.startswith("\ufeff"):
        _fail(title, "byte_order_mark", "a byte order mark is not allowed")
    if _too_deep(text):
        _fail(title, "too_deep", f"the document is nested more than {MAX_DEPTH} levels deep")
    try:
        json.loads(
            text,
            object_pairs_hook=_reject_duplicates,
            parse_constant=_reject_constant,
            # Numbers only matter to the duplicate-key scan: keep their text, so 1e1000 does not become inf and a
            # very long integer does not hit Python's int-digit limit. The model then rejects them as non-strings.
            parse_float=str,
            parse_int=str,
        )
    except _DuplicateKeyError as exc:
        _fail(title, "duplicate_key", str(exc))
    except RecursionError:
        # Fallback: the depth check above should make this unreachable.
        _fail(title, "too_deep", "the document is nested too deeply")
    except ValueError as exc:  # JSONDecodeError and the NaN/Infinity constants
        _fail(title, "json_invalid", f"the document is not valid JSON: {exc}")
    return text


def check_json(data: str | bytes | bytearray) -> None:
    """Check that ``data`` is UTF-8 JSON (no BOM) without a repeated key at any nesting level.

    ``Messwert.model_validate_json`` and ``Zaehlerstand.model_validate_json`` call this themselves. Call it
    first when you validate in another way (``TypeAdapter(...).validate_json``, a model that contains a
    ``Messwert``, ``__pydantic_validator__``): those paths go straight to pydantic's parser, which keeps the
    last of two equal keys without a word. Other problems (wrong types, unknown keys) are left to validation.

    Raises:
        pydantic.ValidationError: with the error type ``not_utf8``, ``byte_order_mark``, ``json_invalid``,
            ``duplicate_key`` or ``too_deep``.
    """
    _read(data, "check_json")


class Model(BaseModel):
    """Frozen, strict model.

    ``extra="forbid"``: a typo must fail loudly, and a new field means all consumers update together.

    Limits, as with Go's ``json.Unmarshal`` (which a wrapper cannot guard either): only
    :meth:`model_validate_json` of a model class rejects duplicate keys. Other entry points do not; run
    :func:`mdq_models.check_json` first. ``model_copy(update=...)`` does not validate (pydantic's default),
    so build a new instance with ``model_validate`` when the rules matter.
    """

    model_config = ConfigDict(frozen=True, extra="forbid", strict=True)

    @classmethod
    def model_validate_json(cls, json_data: str | bytes | bytearray, **kwargs: Any) -> Self:
        """Like :meth:`pydantic.BaseModel.model_validate_json`, but a repeated key is an error.

        Pydantic keeps the last of two equal keys without a word. The contract rejects them (a duplicate is
        always a bug in the producer), so the document is read once with a hook that raises on a repeated key,
        and only then handed to pydantic.

        Raises:
            pydantic.ValidationError: for every problem, including invalid JSON and duplicate keys.
        """
        text = _read(json_data, cls.__name__)
        return super().model_validate_json(text, **kwargs)
