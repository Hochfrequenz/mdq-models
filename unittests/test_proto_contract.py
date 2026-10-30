"""Drift checks: the hand-written proto, the hand-written models and the pinned files agree.

The proto is the published gRPC contract; consumers map proto messages to the models by
hand. These tests make sure that a field or enum value added to one side is added to the others (and
that field numbers, which are the gRPC wire contract, never change). ``proto_contract_test.go`` is the
Go counterpart: it pins the same field numbers and enum values and checks them against the Go types.
"""

import json
import re
from enum import StrEnum
from pathlib import Path
from typing import Any

import pytest
from pydantic import BaseModel

from mdq_models import Herkunft, Messwert, Profiltyp, Richtung, Status, Zaehlerstand

ROOT = Path(__file__).resolve().parent.parent
PROTO_DIR = ROOT / "proto" / "mdq" / "v1"
#: Message -> field -> "<label and type> = <number>". Field numbers are the wire contract of gRPC: they
#: must never change; new fields are added here together with the proto.
PINNED_FIELDS = ROOT / "testdata" / "proto_fields.json"
PINNED_ENUMS = ROOT / "testdata" / "proto_enums.json"

_BLOCK_COMMENT = re.compile(r"/\*.*?\*/", re.DOTALL)
_LINE_COMMENT = re.compile(r"//[^\n]*")
_BLOCK_OPEN = re.compile(r"^(message|enum) (\w+) \{(\s*\})?$")
_TOP_LEVEL = re.compile(r"^(syntax|package|import|option)\b[^;]*;$")
_INNER_SKIP = re.compile(r"^(option|reserved)\b[^;]*;$")
_FIELD_DECL = re.compile(
    r"^((?:optional\s+|repeated\s+)?(?:map\s*<[^>]+>|[\w.]+))\s+(\w+)\s*=\s*(\d+)\s*(?:\[[^\]]*\])?\s*;$"
)
_ENUM_VALUE = re.compile(r"^(\w+)\s*=\s*(-?\d+)\s*(?:\[[^\]]*\])?\s*;$")
_MAP_SPACES = re.compile(r"\s*([<,>])\s*")


def parse_proto(text: str) -> tuple[dict[str, dict[str, str]], dict[str, dict[str, int]]]:
    """Parse proto3 text into message -> field -> "<label type> = <number>" and enum -> constant -> number.

    Raises ``ValueError`` on any line it cannot classify (nested messages, oneof, multi-line statements, ...):
    a drift test must never skip a declaration silently, so new syntax has to be taught to the parser first.
    Field options are accepted and ignored.
    """
    text = _LINE_COMMENT.sub("", _BLOCK_COMMENT.sub("", text))
    messages: dict[str, dict[str, str]] = {}
    enums: dict[str, dict[str, int]] = {}
    kind = name = ""
    for number, raw in enumerate(text.split("\n"), start=1):
        line = raw.strip()
        where = f"proto line {number} {line!r}"
        if not line:
            continue
        if not kind and _TOP_LEVEL.match(line):
            continue
        if not kind and (opened := _BLOCK_OPEN.match(line)):
            block_kind, block_name, empty = opened.group(1), opened.group(2), opened.group(3)
            if block_name in messages or block_name in enums:
                raise ValueError(f"{where}: {block_name} declared twice")
            if block_kind == "message":
                messages[block_name] = {}
            else:
                enums[block_name] = {}
            if not empty:
                kind, name = block_kind, block_name
        elif kind and line == "}":
            kind = name = ""
        elif kind and _INNER_SKIP.match(line):
            continue
        elif kind == "message" and (field := _FIELD_DECL.match(line)):
            declared = _MAP_SPACES.sub(r"\1", " ".join(field.group(1).split()))
            if field.group(2) in messages[name]:
                raise ValueError(f"{where}: field {name}.{field.group(2)} declared twice")
            messages[name][field.group(2)] = f"{declared} = {field.group(3)}"
        elif kind == "enum" and (value := _ENUM_VALUE.match(line)):
            if value.group(1) in enums[name]:
                raise ValueError(f"{where}: constant {name}.{value.group(1)} declared twice")
            enums[name][value.group(1)] = int(value.group(2))
        else:
            raise ValueError(f"{where}: not understood by the drift-test parser (inside {kind!r} {name!r})")
    if kind:
        raise ValueError(f"proto: {kind} {name} is not closed")
    return messages, enums


MODELS: dict[str, type[BaseModel]] = {"Messwert": Messwert, "Zaehlerstand": Zaehlerstand}
ENUMS: dict[str, type[StrEnum]] = {
    "Status": Status,
    "Herkunft": Herkunft,
    "Profiltyp": Profiltyp,
    "Richtung": Richtung,
}


def _parse_proto_files() -> tuple[dict[str, dict[str, str]], dict[str, dict[str, int]]]:
    files = sorted(PROTO_DIR.glob("*.proto"))
    assert files, f"no .proto files in {PROTO_DIR}"
    messages: dict[str, dict[str, str]] = {}
    enums: dict[str, dict[str, int]] = {}
    for path in files:
        file_messages, file_enums = parse_proto(path.read_text(encoding="utf-8"))
        for declared in (*file_messages, *file_enums):
            assert declared not in messages and declared not in enums, f"{declared} is declared in two files"
        messages.update(file_messages)
        enums.update(file_enums)
    return messages, enums


def _proto_field_declarations() -> dict[str, dict[str, str]]:
    return _parse_proto_files()[0]


def _proto_enums() -> dict[str, dict[str, int]]:
    return _parse_proto_files()[1]


def _pinned_enums() -> dict[str, Any]:
    result: dict[str, Any] = json.loads(PINNED_ENUMS.read_bytes())
    return result


def test_proto_field_numbers_and_types_are_pinned() -> None:
    pinned: dict[str, dict[str, str]] = json.loads(PINNED_FIELDS.read_bytes())
    current = _proto_field_declarations()
    assert set(current) == set(pinned), "messages differ from the pinned file"
    for message, fields in pinned.items():
        for field, declaration in fields.items():
            assert current[message].get(field) == declaration, (
                f"{message}.{field} changed from {declaration!r}: field numbers and types must never change"
            )
    for message, fields in current.items():
        numbers = [declaration.rsplit(" = ", 1)[1] for declaration in fields.values()]
        assert len(numbers) == len(set(numbers)), f"{message} reuses a field number"
        for field, declaration in fields.items():
            assert field in pinned[message], (
                f"pin the new field {message}.{field} ({declaration}) in {PINNED_FIELDS.name}"
            )


@pytest.mark.parametrize("message", sorted(MODELS))
def test_proto_message_fields_equal_model_fields(message: str) -> None:
    proto_fields = set(_proto_field_declarations()[message])
    model_fields = set(MODELS[message].model_fields)
    assert proto_fields - model_fields == set(), f"{message}: proto fields missing in the model"
    assert model_fields - proto_fields == set(), f"{message}: model fields missing in the proto"


def test_every_proto_message_is_checked() -> None:
    assert set(_proto_field_declarations()) == set(MODELS)


def test_proto_enums_match_the_pinned_enums_file() -> None:
    pinned = _pinned_enums()
    proto = _proto_enums()
    assert set(proto) == set(pinned) == set(ENUMS)
    for name, constants in proto.items():
        zero = pinned[name]["unspecified"]
        expected = {zero: 0, **{v["name"]: v["number"] for v in pinned[name]["values"]}}
        assert constants == expected, name


@pytest.mark.parametrize("name", sorted(ENUMS))
def test_pinned_enum_json_values_match_the_python_enum(name: str) -> None:
    values = [v["json"] for v in _pinned_enums()[name]["values"]]
    assert values == [member.value for member in ENUMS[name]]


@pytest.mark.parametrize("name", sorted(ENUMS))
def test_proto_enum_names_derive_from_the_python_enum(name: str) -> None:
    """Both directions: every proto constant has a member and every member has a proto constant."""
    constants = _proto_enums()[name]
    prefix = f"{name.upper()}_"
    unspecified = f"{prefix}UNSPECIFIED"
    assert constants[unspecified] == 0
    from_proto = {c.removeprefix(prefix).lower() for c in constants if c != unspecified}
    assert all(c.startswith(prefix) for c in constants), name
    assert from_proto == {member.value for member in ENUMS[name]}


@pytest.mark.parametrize("name", sorted(ENUMS))
def test_unspecified_has_no_json_value(name: str) -> None:
    pinned = _pinned_enums()[name]
    assert pinned["unspecified"] == f"{name.upper()}_UNSPECIFIED"
    assert all(v["number"] != 0 and v["name"] != pinned["unspecified"] for v in pinned["values"])
    for member in ENUMS[name]:
        assert "unspecified" not in member.value
    with pytest.raises(ValueError):
        ENUMS[name]("unspecified")
    with pytest.raises(ValueError):
        ENUMS[name]("")


SNIPPET = """
syntax = "proto3";
package x.v1;
import "google/protobuf/timestamp.proto";
option java_package = "x";

/* block comment
message Hidden { string hidden = 1; }
*/
message Empty {}
message All {
  // string commented_out = 9;
  string plain = 1;
  optional string opt = 2;
  repeated google.protobuf.Timestamp rep = 3;
  map<string, int64> kv = 4;
  string extra = 5 [deprecated = true];
  reserved 6, 7;
  option deprecated = true;
}
enum Neg {
  NEG_UNSPECIFIED = 0;
  NEG_NEU = -1;
  NEG_OLD = 2 [deprecated = true];
  reserved 3;
}
"""


def test_parse_proto_snippet() -> None:
    messages, enums = parse_proto(SNIPPET)
    assert messages == {
        "Empty": {},
        "All": {
            "plain": "string = 1",
            "opt": "optional string = 2",
            "rep": "repeated google.protobuf.Timestamp = 3",
            "kv": "map<string,int64> = 4",
            "extra": "string = 5",
        },
    }
    assert enums == {"Neg": {"NEG_UNSPECIFIED": 0, "NEG_NEU": -1, "NEG_OLD": 2}}


@pytest.mark.parametrize(
    "bad",
    [
        "message M {\n  oneof o {\n    string a = 1;\n  }\n}\n",
        "message M {\n  message Inner {}\n}\n",
        "message M {\n  string a =\n 1;\n}\n",
        "enum E {\n  E_A = 0; E_B = 1;\n}\n",
        "message M {\n  string a = 1;\n",
        "message M {\n  string a = 1;\n  string b = 1x;\n}\n",
        "service S {\n  rpc A(M) returns (M);\n}\n",
    ],
)
def test_parse_proto_fails_loudly_on_unknown_syntax(bad: str) -> None:
    with pytest.raises(ValueError):
        parse_proto(bad)
