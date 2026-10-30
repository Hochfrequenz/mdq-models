package mdqmodels_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const fixtureDir = "testdata"

var fixtureKinds = []string{"messwert", "zaehlerstand"}

var messwertKeys = []string{"malo", "timestamp", "resolution", "value", "status", "herkunft", "profiltyp", "richtung"}
var zaehlerstandKeys = []string{"malo", "timestamp", "value"}

var (
	fixtureMaLoRe      = regexp.MustCompile(`^[1-9][0-9]{10}$`)
	fixtureTimestampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)
	// plan 0.3: shortest canonical form, at most 12 integer digits, at most 6 decimal places (negative zero is checked separately).
	fixtureDecimalRe = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,11})(\.[0-9]{0,5}[1-9])?$`)
)

var fixtureEnums = map[string][]string{
	"status":    {"wahr", "vorlaeufig", "ersatzwert", "fehlend"},
	"herkunft":  {"fremd", "mqa"},
	"profiltyp": {"haushalt", "gewerbe", "industrie", "unbekannt"},
	"richtung":  {"bezug", "einspeisung"},
}

// fixturesReordered are valid fixtures that deliberately do not use the contract key order
// (parsing accepts any order; the order matters only for output).
var fixturesReordered = []string{"messwert/valid/key_order_reversed.json", "zaehlerstand/valid/key_order_reversed.json"}

// fixturesWithDuplicates are the only fixtures that repeat a key; duplicate keys are rejected on purpose.
var fixturesWithDuplicates = []string{"messwert/invalid/duplicate_key.json", "zaehlerstand/invalid/duplicate_key.json"}

// fixturesNotJSON are the invalid fixtures about the text encoding: a byte order mark, UTF-16 and
// invalid UTF-8 bytes. They are not (valid UTF-8) JSON by design, so the tests that decode fixtures
// as JSON skip them; the parsers must still reject them.
var fixturesNotJSON = []string{
	"messwert/invalid/utf8_bom.json", "messwert/invalid/utf16_encoded.json", "messwert/invalid/invalid_utf8_bytes.json",
	"zaehlerstand/invalid/utf8_bom.json", "zaehlerstand/invalid/utf16_encoded.json", "zaehlerstand/invalid/invalid_utf8_bytes.json",
}

func fixtureRead(t *testing.T, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{fixtureDir}, parts...)...))
	require.NoError(t, err)
	return b
}

// fixtureFiles lists the fixtures of one validity as paths relative to testdata/, for example
// "messwert/valid/wahr_fremd.json".
func fixtureFiles(t *testing.T, validity string) []string {
	t.Helper()
	var names []string
	for _, kind := range fixtureKinds {
		files, err := filepath.Glob(filepath.Join(fixtureDir, kind, validity, "*.json"))
		require.NoError(t, err)
		require.NotEmpty(t, files, "no fixtures in %s/%s", kind, validity)
		for _, f := range files {
			names = append(names, kind+"/"+validity+"/"+filepath.Base(f))
		}
	}
	sort.Strings(names)
	return names
}

// reasonKey turns "messwert/invalid/x.json" into the key used in invalid_reasons.json.
func reasonKey(path string) string {
	return filepath.Dir(filepath.Dir(path)) + "/" + filepath.Base(path)
}

// fixtureCheckDigit is a minimal copy of the BDEW check-digit algorithm, so that the shared cases are
// proven correct independently of the models.
func fixtureCheckDigit(first10 string) (int, bool) {
	if len(first10) != 10 {
		return 0, false
	}
	total := 0
	for i := 0; i < 10; i++ {
		c := first10[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		d := int(c - '0')
		if i%2 == 1 {
			d *= 2
		}
		total += d
	}
	return (10 - total%10) % 10, true
}

func fixtureIsValidMaLo(id string) bool {
	if !fixtureMaLoRe.MatchString(id) {
		return false
	}
	cd, ok := fixtureCheckDigit(id[:10])
	return ok && int(id[10]-'0') == cd
}

// objectKeys returns the keys of a JSON object in document order.
func objectKeys(t *testing.T, data []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('{'), tok, "top level must be an object")
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		require.NoError(t, err)
		keys = append(keys, k.(string))
		var skip json.RawMessage
		require.NoError(t, dec.Decode(&skip))
	}
	return keys
}

// hasDuplicateKeys reports whether any object anywhere in the document repeats a key. A plain
// json.Unmarshal into a map silently keeps the last value.
func hasDuplicateKeys(t *testing.T, data []byte) bool {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // numbers such as 1e1000 do not fit a float64
	dup, err := scanDuplicates(dec)
	require.NoError(t, err)
	return dup
}

func scanDuplicates(dec *json.Decoder) (bool, error) {
	tok, err := dec.Token()
	if err != nil {
		return false, err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return false, nil
	}
	dup := false
	switch delim {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return false, err
			}
			key := k.(string)
			if seen[key] {
				dup = true
			}
			seen[key] = true
			d, err := scanDuplicates(dec)
			if err != nil {
				return false, err
			}
			dup = dup || d
		}
	case '[':
		for dec.More() {
			d, err := scanDuplicates(dec)
			if err != nil {
				return false, err
			}
			dup = dup || d
		}
	}
	_, err = dec.Token() // closing delimiter
	return dup, err
}

// fixtureDuplicateFree decodes a shared file and fails on duplicate keys.
func fixtureDuplicateFree(t *testing.T, name string) []byte {
	t.Helper()
	data := fixtureRead(t, name)
	require.False(t, hasDuplicateKeys(t, data), "%s contains a duplicate key", name)
	return data
}

func TestDuplicateKeyDetection(t *testing.T) {
	assert.True(t, hasDuplicateKeys(t, []byte(`{"a": 1, "a": 2}`)))
	assert.True(t, hasDuplicateKeys(t, []byte(`{"a": {"b": 1, "b": 2}}`)))
	assert.True(t, hasDuplicateKeys(t, []byte(`[{"a": 1, "a": 2}]`)))
	assert.False(t, hasDuplicateKeys(t, []byte(`{"a": {"b": 1}, "b": 2}`)))
	assert.False(t, hasDuplicateKeys(t, []byte(`"x"`)))
}

func TestDuplicateKeysOccurExactlyInTheDuplicateKeyFixtures(t *testing.T) {
	var found []string
	for _, name := range append(fixtureFiles(t, "valid"), fixtureFiles(t, "invalid")...) {
		if inList(fixturesNotJSON, name) {
			continue
		}
		if hasDuplicateKeys(t, fixtureRead(t, name)) {
			found = append(found, name)
		}
	}
	assert.ElementsMatch(t, fixturesWithDuplicates, found)
}

func TestValidFixturesAreJSONObjects(t *testing.T) {
	for _, name := range fixtureFiles(t, "valid") {
		t.Run(name, func(t *testing.T) {
			var v map[string]any
			require.NoError(t, json.Unmarshal(fixtureRead(t, name), &v), "top level must be an object")
			require.NotNil(t, v, "top level must be an object, not null") // Unmarshal accepts null into a map
		})
	}
}

// Invalid fixtures need not be objects ([], null, "x"), but they must be parseable JSON.
func TestInvalidFixturesAreJSON(t *testing.T) {
	for _, name := range fixtureFiles(t, "invalid") {
		if inList(fixturesNotJSON, name) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			assert.True(t, json.Valid(fixtureRead(t, name)))
		})
	}
}

// decodeUsingNumber is json.Unmarshal into any, with numbers kept as json.Number so that huge ones
// (1e1000) do not fail.
func decodeUsingNumber(t *testing.T, data []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc any
	require.NoError(t, dec.Decode(&doc))
	return doc
}

func inList(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// fixtureErrors is the reference validator for plan 0.1-0.5, deliberately independent of the models.
// An empty result means valid.
func fixtureErrors(kind string, doc any) []string {
	m, ok := doc.(map[string]any)
	if !ok {
		return []string{"top level is not an object"}
	}
	wantKeys := map[string][]string{"messwert": messwertKeys, "zaehlerstand": zaehlerstandKeys}[kind]
	if len(m) != len(wantKeys) {
		return []string{"key set"}
	}
	for _, k := range wantKeys {
		if _, present := m[k]; !present {
			return []string{"key set"}
		}
	}
	var errs []string
	if malo, ok := m["malo"].(string); !ok || !fixtureIsValidMaLo(malo) {
		errs = append(errs, "malo")
	}
	errs = append(errs, fixtureTimestampErrors(kind, m["timestamp"])...)
	switch v := m["value"].(type) {
	case nil:
		if kind == "zaehlerstand" || m["status"] != "fehlend" {
			errs = append(errs, "value is null")
		}
	case string:
		if !fixtureDecimalRe.MatchString(v) || v == "-0" {
			errs = append(errs, "value is not a canonical decimal with at most 6 decimal places")
		} else if kind == "messwert" && m["status"] == "fehlend" {
			errs = append(errs, "value set for fehlend")
		}
	default:
		errs = append(errs, "value type")
	}
	if kind == "messwert" {
		errs = append(errs, fixtureMesswertErrors(m)...)
	}
	return errs
}

func fixtureTimestampErrors(kind string, v any) []string {
	ts, ok := v.(string)
	if !ok || !fixtureTimestampRe.MatchString(ts) {
		return []string{"timestamp shape"}
	}
	parsed, err := time.Parse("2006-01-02T15:04:05Z", ts)
	if err != nil {
		return []string{"timestamp is not a calendar time"}
	}
	var errs []string
	if parsed.Year() < 1900 || parsed.Year() > 9999 {
		errs = append(errs, "timestamp year")
	}
	if kind == "messwert" && (parsed.Minute()%15 != 0 || parsed.Second() != 0) {
		errs = append(errs, "timestamp is off the 15-minute raster")
	}
	return errs
}

func fixtureMesswertErrors(m map[string]any) []string {
	var errs []string
	if m["resolution"] != "PT15M" {
		errs = append(errs, "resolution")
	}
	for _, key := range []string{"status", "profiltyp", "richtung"} {
		if s, ok := m[key].(string); !ok || !inList(fixtureEnums[key], s) {
			errs = append(errs, key)
		}
	}
	herkunft, isString := m["herkunft"].(string)
	if m["herkunft"] != nil && (!isString || !inList(fixtureEnums["herkunft"], herkunft)) {
		errs = append(errs, "herkunft")
	}
	// one violation per fixture: a null herkunft is reported once, also for wahr
	switch {
	case m["status"] == "fehlend":
		if m["herkunft"] != nil {
			errs = append(errs, "herkunft set for fehlend")
		}
	case m["herkunft"] == nil:
		if inList(fixtureEnums["status"], fmt.Sprint(m["status"])) {
			errs = append(errs, "herkunft null")
		}
	case m["status"] == "wahr" && isString && inList(fixtureEnums["herkunft"], herkunft) && herkunft != "fremd":
		errs = append(errs, "wahr requires fremd")
	}
	return errs
}

func TestReferenceValidatorSanity(t *testing.T) {
	zs := func(ts, value string) any {
		return map[string]any{"malo": "68283561554", "timestamp": ts, "value": value}
	}
	assert.Empty(t, fixtureErrors("zaehlerstand", zs("2026-01-15T10:17:23Z", "0.000001")))
	assert.NotEmpty(t, fixtureErrors("zaehlerstand", zs("2026-01-15T10:17:23Z", "0.0000001")))
	assert.NotEmpty(t, fixtureErrors("zaehlerstand", zs("2026-01-15T10:17:23Z", "1.10")))
	assert.NotEmpty(t, fixtureErrors("zaehlerstand", zs("1900-02-29T00:00:00Z", "1")))
	assert.Empty(t, fixtureErrors("zaehlerstand", zs("2000-02-29T00:00:00Z", "1")))
	assert.NotEmpty(t, fixtureErrors("zaehlerstand", zs("2026-01-15T10:17:23Z", "-0")))
}

// dropRepeatedKeys removes later occurrences of a repeated key from a top-level object, keeping the first.
func dropRepeatedKeys(t *testing.T, data []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	require.NoError(t, err)
	require.Equal(t, json.Delim('{'), tok)
	doc := map[string]any{}
	for dec.More() {
		k, err := dec.Token()
		require.NoError(t, err)
		var v any
		require.NoError(t, dec.Decode(&v))
		if _, seen := doc[k.(string)]; !seen {
			doc[k.(string)] = v
		}
	}
	return doc
}

func TestInvalidFixturesViolateExactlyOneRule(t *testing.T) {
	for _, name := range fixtureFiles(t, "invalid") {
		if inList(fixturesNotJSON, name) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			data := fixtureRead(t, name)
			kind := strings.SplitN(name, "/", 2)[0]
			if hasDuplicateKeys(t, data) {
				// only the repeated key makes it invalid: valid once the later occurrence is dropped
				assert.Empty(t, fixtureErrors(kind, dropRepeatedKeys(t, data)))
				return
			}
			doc := decodeUsingNumber(t, data)
			if _, isObject := doc.(map[string]any); !isObject {
				assert.NotEmpty(t, fixtureErrors(kind, doc))
				return
			}
			assert.Len(t, fixtureErrors(kind, doc), 1)
		})
	}
}

func TestInvalidFixturesAndReasonsMatch(t *testing.T) {
	var reasons map[string]string
	require.NoError(t, json.Unmarshal(fixtureDuplicateFree(t, "invalid_reasons.json"), &reasons))
	var want, have []string
	for _, name := range fixtureFiles(t, "invalid") {
		want = append(want, reasonKey(name))
	}
	for name, reason := range reasons {
		have = append(have, name)
		assert.NotEmpty(t, reason, name)
		assert.NotContains(t, reason, "\n", name)
	}
	assert.ElementsMatch(t, want, have, "every invalid fixture needs a reason and every reason a fixture")
}

func TestValidFixturesFollowTheContractShape(t *testing.T) {
	wantKeys := map[string][]string{"messwert": messwertKeys, "zaehlerstand": zaehlerstandKeys}
	for _, name := range fixtureFiles(t, "valid") {
		t.Run(name, func(t *testing.T) {
			data := fixtureRead(t, name)
			kind := filepath.Dir(filepath.Dir(name))
			if inList(fixturesReordered, name) {
				assert.NotEqual(t, wantKeys[kind], objectKeys(t, data))
				assert.ElementsMatch(t, wantKeys[kind], objectKeys(t, data))
			} else {
				assert.Equal(t, wantKeys[kind], objectKeys(t, data))
			}
			doc := decodeUsingNumber(t, data)
			assert.Empty(t, fixtureErrors(kind, doc))
		})
	}
}

func TestMaLoIDsFixtureAgreesWithCheckDigit(t *testing.T) {
	var cases struct {
		Valid   []string                      `json:"valid"`
		Invalid []struct{ ID, Reason string } `json:"invalid"`
		Digits  []struct {
			FirstTen   string `json:"first_ten"`
			CheckDigit int    `json:"check_digit"`
		} `json:"check_digits"`
		BadInput []struct {
			FirstTen string `json:"first_ten"`
			Reason   string `json:"reason"`
		} `json:"check_digit_invalid_input"`
	}
	require.NoError(t, json.Unmarshal(fixtureDuplicateFree(t, "malo_ids.json"), &cases))
	require.NotEmpty(t, cases.Valid)
	require.NotEmpty(t, cases.Invalid)
	// 51238696781 is the BDEW example ID from the MaLo-ID application guide (check-digit reference).
	assert.Contains(t, cases.Valid, "51238696781")
	for _, id := range cases.Valid {
		assert.True(t, fixtureIsValidMaLo(id), id)
	}
	for _, c := range cases.Invalid {
		assert.False(t, fixtureIsValidMaLo(c.ID), "%q (%s)", c.ID, c.Reason)
		assert.NotEmpty(t, c.Reason)
	}
	for _, c := range cases.Digits {
		got, ok := fixtureCheckDigit(c.FirstTen)
		assert.True(t, ok, c.FirstTen)
		assert.Equal(t, c.CheckDigit, got, c.FirstTen)
	}
	require.NotEmpty(t, cases.BadInput)
	for _, c := range cases.BadInput {
		_, ok := fixtureCheckDigit(c.FirstTen)
		assert.False(t, ok, "%q (%s)", c.FirstTen, c.Reason)
	}
}

func TestHerkunftStatusFixtureIsComplete(t *testing.T) {
	var cases []struct {
		Status   string  `json:"status"`
		Herkunft *string `json:"herkunft"`
		OK       bool    `json:"ok"`
	}
	require.NoError(t, json.Unmarshal(fixtureDuplicateFree(t, "herkunft_status.json"), &cases))
	assert.GreaterOrEqual(t, len(cases), 12)
	known := map[string]bool{"wahr": true, "vorlaeufig": true, "ersatzwert": true, "fehlend": true}
	seen := map[string]bool{}
	for _, c := range cases {
		h := "null"
		if c.Herkunft != nil {
			h = "\"" + *c.Herkunft + "\""
		}
		key := c.Status + "/" + h
		assert.False(t, seen[key], "duplicate %s", key)
		seen[key] = true
		// Reference rule: unknown status or herkunft (including "") is never ok.
		validHerkunft := c.Herkunft == nil || *c.Herkunft == "fremd" || *c.Herkunft == "mqa"
		var want bool
		switch {
		case !known[c.Status] || !validHerkunft:
			want = false
		case c.Status == "fehlend":
			want = c.Herkunft == nil
		case c.Status == "wahr":
			want = c.Herkunft != nil && *c.Herkunft == "fremd"
		default:
			want = c.Herkunft != nil
		}
		assert.Equal(t, want, c.OK, key)
	}
	for _, status := range []string{"wahr", "vorlaeufig", "ersatzwert", "fehlend"} {
		for _, h := range []string{"null", "\"fremd\"", "\"mqa\""} {
			assert.True(t, seen[status+"/"+h], "missing combination %s/%s", status, h)
		}
	}
}

func TestProtoEnumsFixtureShape(t *testing.T) {
	var enums map[string]struct {
		Unspecified string `json:"unspecified"`
		Values      []struct {
			Name   string `json:"name"`
			Number int    `json:"number"`
			JSON   string `json:"json"`
		} `json:"values"`
	}
	require.NoError(t, json.Unmarshal(fixtureDuplicateFree(t, "proto_enums.json"), &enums))
	require.Len(t, enums, 4)
	for name, e := range enums {
		prefix := map[string]string{"Status": "STATUS", "Herkunft": "HERKUNFT", "Profiltyp": "PROFILTYP", "Richtung": "RICHTUNG"}[name]
		require.NotEmpty(t, prefix, name)
		assert.Equal(t, prefix+"_UNSPECIFIED", e.Unspecified)
		require.NotEmpty(t, e.Values)
		for i, v := range e.Values {
			assert.Equal(t, i+1, v.Number)
			assert.NotEmpty(t, v.JSON)
			assert.Equal(t, prefix+"_"+strings.ToUpper(v.JSON), v.Name)
		}
	}
}
