# mdq-models

![Go Tests](https://github.com/Hochfrequenz/mdq-models/workflows/Go%20Tests/badge.svg)
![Go Lint](https://github.com/Hochfrequenz/mdq-models/workflows/Go%20Lint/badge.svg)
[![Go Reference](https://pkg.go.dev/badge/github.com/hochfrequenz/mdq-models.svg)](https://pkg.go.dev/github.com/hochfrequenz/mdq-models)
![Python Tests](https://github.com/Hochfrequenz/mdq-models/workflows/Python%20Tests/badge.svg)
![Python Coverage](https://github.com/Hochfrequenz/mdq-models/workflows/Python%20Coverage/badge.svg)
![Python Lint](https://github.com/Hochfrequenz/mdq-models/workflows/Python%20Lint/badge.svg)
![Python Formatting](https://github.com/Hochfrequenz/mdq-models/workflows/Python%20Formatting/badge.svg)
![Proto](https://github.com/Hochfrequenz/mdq-models/workflows/Proto/badge.svg)
![Packaging Test](https://github.com/Hochfrequenz/mdq-models/workflows/Packaging%20Test/badge.svg)

Data models of the **canonical meter-reading schema, version 1**: `Messwert` (one value per market
location and quarter hour) and `Zaehlerstand` (a meter reading, the second input of the schema).

This repository holds

- **Go** types (package `mdqmodels`, module `github.com/hochfrequenz/mdq-models`),
- **Python** frozen pydantic v2 models (package `mdq_models`) and an optional **Polars** table schema
  (`mdq_models.polars`, extra `polars`),
- the shared **fixtures** in [`testdata/`](testdata) that define the JSON contract, and
- the **gRPC contract** in [`proto/mdq/v1`](proto/mdq/v1).

Everything is **written by hand**. No code is generated and no generated code is committed: every
type, validator and (un)marshaller is ordinary, readable code. Field names and enum values are
German (`malo`, `herkunft`, `vorlaeufig`, ...) and are part of the contract. Both languages parse every
valid fixture, write it back byte-for-byte as pinned in `testdata/serialized.golden` and reject every
invalid fixture, so Go and Python agree on the contract by construction.

## The contract in short

| Topic | Rule |
|---|---|
| `Messwert` keys | In this order on output: `malo`, `timestamp`, `resolution`, `value`, `status`, `herkunft`, `profiltyp`, `richtung`. All eight are **required**; `value` and `herkunft` are required-but-nullable (always written, `null` when unset). A missing key is rejected, also when its value would be `null`. |
| `Zaehlerstand` keys | `malo`, `timestamp`, `value`, all required; `value` is never `null`. |
| Strict reading | **Unknown keys and duplicate keys are rejected** (at every nesting level), so a typo fails loudly and two parsers cannot read different data from the same bytes. Key order matters only for output; parsing accepts any order. The top level must be a JSON object. Invalid UTF-8, a byte order mark and UTF-16 are rejected. |
| `malo` | `^[1-9][0-9]{10}$` (ASCII digits only) with a valid BDEW check digit. |
| `timestamp` | RFC 3339, upper-case `T`, offset exactly `Z`, whole seconds, years 1900-9999, ASCII digits only. For `Messwert` it is the **start of the quarter hour** (`00:00` is the first of a day, `23:45` the last; it equals the start of a `[from, to)` interval) and must be on the raster (minute divisible by 15, second 0). For `Zaehlerstand` it is a point in time with any whole second. Built in code, a timestamp in any time zone is accepted and written in UTC; a naive timestamp is rejected. |
| `resolution` | Only `"PT15M"` in v1. |
| `value` | A decimal **string** in its shortest canonical form (`"160"`, `"0.022"`, `"-3.5"`), never a JSON number. At most 12 integer digits and at most 6 decimals (counted after stripping trailing zeros). No exponent, no plus sign, no leading zeros, no trailing zeros after the point (`"1.0"` is rejected), no negative zero, no separators. Negative values are allowed. Producers that compute in floating point must round to 6 decimals first: `"0.1234567"` is rejected, not rounded. |
| `status` | `wahr`, `vorlaeufig`, `ersatzwert`, `fehlend`. Case-sensitive; the retired `gemessen` is rejected. |
| `herkunft` | `fremd`, `mqa`. `null` **iff** `status` is `fehlend`; `wahr` requires `fremd`. |
| `value` and `status` | `value` is `null` **iff** `status` is `fehlend`. |
| `profiltyp` | `haushalt`, `gewerbe`, `industrie`, `unbekannt`. |
| `richtung` | `bezug`, `einspeisung`. |
| Decimals built in code | Go `NewDecimal` and Python's `Decimal` input normalise trailing zeros (`"1.5000000"` becomes `"1.5"`) and reject more than 6 decimals, more than 12 integer digits, `NaN` and `Infinity`; they never round. |
| JSON vs. proto | The `.proto` files are the gRPC contract only. The JSON form is produced by the hand-written models, **never by protojson** (which would write `"STATUS_VORLAEUFIG"` and omit unset `optional` fields instead of writing `null`). |

## Install

```bash
pip install "mdq-models[polars]"   # the extra is only needed for mdq_models.polars
go get github.com/hochfrequenz/mdq-models
```

Python 3.11 or newer; `mdq_models` itself depends only on pydantic and never imports polars.

## Go

Read untrusted bytes with `ParseMesswert` / `ParseZaehlerstand`: every failure is a
`*ValidationError`. Plain `json.Unmarshal` reports syntax errors as `*json.SyntaxError` and leaves a
`*Messwert` nil on a top-level `null`.

```go
raw := []byte(`{"malo":"51238696781","timestamp":"2026-03-29T00:45:00Z","resolution":"PT15M",` +
	`"value":"1.5","status":"wahr","herkunft":"fremd","profiltyp":"haushalt","richtung":"bezug"}`)
m, err := mdq.ParseMesswert(raw) // every failure is a *mdq.ValidationError
if err != nil {
	log.Fatal(err)
}
fmt.Println(m.Malo, m.Timestamp.Format(time.RFC3339), *m.Value, m.Status)

// Build a row in code. NewDecimal drops trailing zeros: "2.50" becomes "2.5".
value, err := mdq.NewDecimal("2.50")
if err != nil {
	log.Fatal(err)
}
mqa := mdq.HerkunftMqa
built := mdq.Messwert{
	Malo:       "51238696781",
	Timestamp:  time.Date(2026, 3, 29, 3, 0, 0, 0, time.FixedZone("CEST", 2*3600)), // written as UTC
	Resolution: mdq.PT15M,
	Value:      &value,
	Status:     mdq.StatusErsatzwert,
	Herkunft:   &mqa,
	Profiltyp:  mdq.ProfiltypHaushalt,
	Richtung:   mdq.RichtungBezug,
}
out, err := built.MarshalJSON() // validates; an invalid row does not marshal
if err != nil {
	log.Fatal(err)
}
fmt.Println(string(out))

// Reading is strict: duplicate (and unknown) keys are rejected.
_, err = mdq.ParseMesswert([]byte(`{"malo":"51238696781","malo":"51238696781"}`))
var invalid *mdq.ValidationError
fmt.Println(errors.As(err, &invalid))
// Output:
// 51238696781 2026-03-29T00:45:00Z 1.5 wahr
// {"malo":"51238696781","timestamp":"2026-03-29T01:00:00Z","resolution":"PT15M","value":"2.5","status":"ersatzwert","herkunft":"mqa","profiltyp":"haushalt","richtung":"bezug"}
// true
```

(with `import mdq "github.com/hochfrequenz/mdq-models"`). This snippet is the body of
`ExampleParseMesswert` in [`example_test.go`](example_test.go); a test fails if the two drift apart.
`Zaehlerstand` works the same way. Every type validates itself in `UnmarshalJSON` and refuses to
marshal when invalid; `Validate()` checks values built in code.

## Python

```python
from datetime import datetime, timedelta, timezone
from decimal import Decimal

from pydantic import ValidationError

from mdq_models import Herkunft, Messwert, Profiltyp, Richtung, Status

raw = (
    '{"malo":"51238696781","timestamp":"2026-03-29T00:45:00Z","resolution":"PT15M",'
    '"value":"1.5","status":"wahr","herkunft":"fremd","profiltyp":"haushalt","richtung":"bezug"}'
)
m = Messwert.model_validate_json(raw)  # rejects duplicate and unknown keys
print(m.malo, m.timestamp.isoformat(), m.value, m.status.value)  # 51238696781 2026-03-29T00:45:00+00:00 1.5 wahr

# Build a row in code. Trailing zeros are dropped: Decimal("2.50") is written as "2.5".
built = Messwert(
    malo="51238696781",
    timestamp=datetime(2026, 3, 29, 3, 0, tzinfo=timezone(timedelta(hours=2))),  # written as UTC
    resolution="PT15M",
    value=Decimal("2.50"),
    status=Status.ERSATZWERT,
    herkunft=Herkunft.MQA,
    profiltyp=Profiltyp.HAUSHALT,
    richtung=Richtung.BEZUG,
)
print(built.model_dump_json())  # the same bytes the Go models write

# Reading is strict: duplicate keys are rejected. Only Messwert.model_validate_json checks this by itself; for
# TypeAdapter and nested models call check_json(raw) on the raw document first.
try:
    Messwert.model_validate_json('{"malo":"51238696781","malo":"51238696781"}')
except ValidationError:
    print("rejected")
```

Plain pydantic keeps the last of two duplicate keys; `mdq_models.check_json(raw)` is the explicit check for
paths that bypass `model_validate_json`. Use `Messwert.model_validate_json` (or
`Zaehlerstand.model_validate_json`) for raw JSON. All parse failures are
`pydantic.ValidationError`.

### Polars

`mdq_models.polars` is the table form Python consumers compute with: seven columns (`malo`,
`timestamp`, `value`, `status`, `herkunft`, `profiltyp`, `richtung`, **without** `resolution`, which is
always `PT15M`), `value` as `Float64` and `timestamp` in `Europe/Berlin` (UTC is only the wire format).
The schema is available as `CANONICAL_COLUMNS`, `CANONICAL_DTYPES` and `TIMESTAMP_DTYPE`.

```python polars
from mdq_models.polars import from_frame, to_frame

frame = to_frame([m, built])  # seven columns, timestamps in Europe/Berlin
print(frame["timestamp"][0])  # 2026-03-29 01:45:00+01:00
assert from_frame(frame) == [m, built]  # lossless for |value| < 10^9
```

`Float64` keeps a decimal exactly only up to 15 significant digits, so the table form is bounded:
`to_frame` raises `ValueError` for `|value| >= 10^9` and `from_frame` for a float of that size; neither
rounds silently. `from_frame` converts each float through its shortest representation and quantizes to
6 decimals. Both are strict about the table: unknown columns and wrong dtypes raise `ValueError`.
Importing `mdq_models.polars` without the extra raises an `ImportError` that names it.

The Python snippets above are run by [`unittests/test_readme.py`](unittests/test_readme.py), so they
cannot rot.

## Fixtures

```
testdata/
  messwert/{valid,invalid}/        one JSON document per file (DST days, boundaries, every rejected rule, ...)
  zaehlerstand/{valid,invalid}/
  invalid_reasons.json             why each invalid fixture is rejected
  malo_ids.json                    valid and invalid MaLo IDs, check digits
  herkunft_status.json             all status x herkunft combinations
  proto_enums.json                 proto enum constants, numbers and JSON values
  proto_fields.json                proto messages, field labels, types and numbers
  serialized.golden                the exact bytes both languages write for every valid fixture
```

[`testdata/README.md`](testdata/README.md) documents every file. Consumers in other languages should
run the same fixtures.

`serialized.golden` pins the output bytes (`<fixture>\t<JSON>` per line), so Go and Python output is
identical: compact JSON, keys in contract order, timestamps in UTC with `Z`.
Every invalid fixture has an entry in `testdata/invalid_reasons.json`, so a fixture cannot pass by being
rejected for the wrong reason.

## How CI keeps Go, Python and proto in step

- **Same fixtures.** `fixtures_test.go` and `unittests/test_fixtures.py` read the same files: every valid
  fixture must parse and write back the exact bytes of its `serialized.golden` line, every invalid one
  must be rejected. Go and Python tests run on every pull request (Python 3.11-3.15, with and without the
  `polars` extra).
- **Golden file.** Only Go writes it (`go test -run Golden -update .`); Python compares against it.
- **Proto drift tests.** `proto_contract_test.go` and `unittests/test_proto_contract.py` fail when a proto
  field, its number or label, or an enum constant differs from `testdata/proto_fields.json` /
  `proto_enums.json`, or when proto and models disagree on field names or JSON enum values.
- **`buf`.** The Proto workflow runs `buf build`, `buf lint`, `buf format --diff --exit-code` and, on pull
  requests, `buf breaking` against the base branch.
- **Lint, format, types, spelling.** golangci-lint, ruff, mypy (strict) and codespell.
- **Packaging.** The wheel is built and imported in a fresh environment, with and without the extra.

## Adding a field

A new field must reach every consumer at once, because reading rejects unknown keys. Do it in **one
pull request**:

1. Add the field to the `.proto` message (next free number; never reuse or renumber) and to
   `testdata/proto_fields.json`.
2. Add it to the Go struct (`Validate`, `MarshalJSON`, `UnmarshalJSON`, the key list) and to the Python
   model, in contract key order.
3. Add valid and invalid fixtures (each rule one fixture, plus an entry in `invalid_reasons.json`).
4. Regenerate the golden file with `go test -run Golden -update .`, review the diff and check that
   `uv run pytest` writes the same bytes.
5. If it is a table column, extend `mdq_models.polars` and its tests.
6. Update this README and `testdata/README.md`.

## Versioning

While the major version is 0, minor releases may contain breaking changes (announced in the release
notes). From 1.0 on the JSON and proto contract of `v1` is frozen: a breaking change needs a new proto
package version (`mdq.v2`) and, for Go, a new module path `github.com/hochfrequenz/mdq-models/v2`.
Readers are strict on purpose: unknown and duplicate JSON keys are rejected, so a typo fails
loudly. The price: **adding a JSON field is a breaking change** for every consumer still on an older
version, even though `buf breaking` accepts the additive proto change. Consumers update together;
from 1.0 on, a new field needs a new major version. Releases are tags
(`vX.Y.Z`); the Python package takes its version from the same tags.

## Development

```bash
go test ./...                      # Go
golangci-lint run && golangci-lint fmt --diff
uv sync --group dev --extra polars # Python (the extra is needed for the Polars module and mypy)
uv run pytest
uv run ruff check . && uv run ruff format --check .
uv run mypy src unittests
uv run codespell --ignore-words=domain-specific-terms.txt src unittests README.md doc.go
buf build && buf lint && buf format --diff --exit-code
```

## License

MIT
