# Shared contract fixtures

Go (`fixtures_test.go`) and Python (`unittests/test_fixtures.py`) read the same files. The rules they
encode are in section 0 of `docs/superpowers/plans/2026-10-30-v1-models.md`. MaLo IDs are random
ones from the Hochfrequenz ID generator (`https://markt.lokations.id/json`, valid check digit), so they
look like real IDs. Invalid IDs are derived from them. The one exception is `51238696781`, the BDEW
example ID from the MaLo-ID application guide; it is kept in `malo_ids.json` as a check-digit reference.

## `messwert/` and `zaehlerstand/`

One JSON document per file.

- `valid/*.json`: a JSON object that follows every rule. Keys are in contract order, except the
  `key_order_reversed.json` fixtures, which check that parsing accepts any key order. Timestamps are
  UTC with `Z`. For `messwert`, `timestamp` is the start of the quarter hour.
- `invalid/*.json`: must be rejected. Each violates one rule (the file name and the reason say
  which). The top level need not be an object (`top_level_*.json`). The `duplicate_key.json`
  fixtures are raw JSON that repeats a key; both values would be valid on their own, so a parser
  that keeps the last value would wrongly accept them. They are the only files that contain a
  duplicate key, and both test suites check that.

## `invalid_reasons.json`

Object mapping `"<kind>/<file name>"` (for example `"messwert/unknown_key.json"`) to a one-line
reason. Every invalid fixture has exactly one entry and every entry a fixture. A duplicated entry
fails the tests (a plain JSON parser would silently keep the last one).

## `malo_ids.json`

- `valid`: list of valid MaLo IDs.
- `invalid`: list of `{id, reason}`; each fails the ID rule or the check digit.
- `check_digits`: list of `{first_ten, check_digit}`.
- `check_digit_invalid_input`: list of `{first_ten, reason}`; input for which computing a check digit
  must fail.

## `herkunft_status.json`

List of `{status, herkunft, ok}` (`herkunft` is `null` or a string); `ok` says whether the combination
is allowed. The first 12 entries are all 4 x 3 combinations of the valid values. The rest are never ok:
an unknown status (`"Wahr"`, `"gemessen"`, `"foo"`, `""`) and an unknown herkunft (`"bogus"`, `"FREMD"`).
The empty string `""` is an unknown herkunft, not "unset": only `null` means unset, in both languages.

## `proto_fields.json`

Object keyed by proto message name (`Messwert`, `Zaehlerstand`) with an object from field name to
`"<label type> = <number>"` (for example `"value": "optional string = 4"`). Pins the field numbers and
types of `proto/mdq/v1/*.proto`, which are the gRPC wire contract and must never change. Both
languages compare it with the proto files; a new field is added here together with the proto.

## `proto_enums.json`

Object keyed by enum name (`Status`, `Herkunft`, `Profiltyp`, `Richtung`) with `unspecified` (name of
the zero constant) and `values`: a list of `{name, number, json}` pinning each proto constant, its
number and its JSON string.

## `serialized.golden`

Written by the Go tests (`go test ./... -update`), compared by both languages: the exact bytes the
models write for every valid fixture. Format:

- one line per valid fixture: `<kind>/<file>\t<json>`, where `<kind>` is `messwert` or `zaehlerstand`,
  `<file>` is the fixture's file name (no `valid/` segment) and `\t` is a single tab;
- `<json>` is compact JSON without any spaces, keys in contract order, timestamps in UTC with `Z`;
- lines are sorted by bytes and separated by `\n` (never `\r\n`); the file ends with a `\n`.

Fixture file names match `^[a-z0-9_]+\.json$` (checked by the tests).

## Encoding fixtures

Three invalid fixtures per kind are deliberately not valid UTF-8 JSON text: `utf8_bom.json` (a byte
order mark before the document), `utf16_encoded.json` (UTF-16 with a byte order mark) and
`invalid_utf8_bytes.json` (the byte `0xFF` inside a string). Tests that decode fixtures as JSON skip
them, so they are listed by name (`fixturesNotJSON` in Go). A parser must reject them when given the
raw bytes. Beware: Python's `json.loads(bytes)` accepts a BOM and UTF-16, so the Python models must
check the encoding themselves.
