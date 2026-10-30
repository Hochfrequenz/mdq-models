package mdqmodels_test

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	mdq "github.com/hochfrequenz/mdq-models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kind bundles parse and dump for one model, so the fixture tests treat both alike.
type kind struct {
	parse func([]byte) (any, error)
	dump  func(any) ([]byte, error)
}

var kinds = map[string]kind{
	"messwert": {
		parse: func(b []byte) (any, error) { var v mdq.Messwert; err := json.Unmarshal(b, &v); return v, err },
		dump:  func(v any) ([]byte, error) { return json.Marshal(v) },
	},
	"zaehlerstand": {
		parse: func(b []byte) (any, error) { var v mdq.Zaehlerstand; err := json.Unmarshal(b, &v); return v, err },
		dump:  func(v any) ([]byte, error) { return json.Marshal(v) },
	},
}

func TestValidFixturesParseAndRoundTrip(t *testing.T) {
	for _, name := range fixtureFiles(t, "valid") {
		t.Run(name, func(t *testing.T) {
			k := kinds[strings.SplitN(name, "/", 2)[0]]
			v, err := k.parse(fixtureRead(t, name))
			require.NoError(t, err)
			out, err := k.dump(v)
			require.NoError(t, err)
			// the serialised form parses again to the same value
			v2, err := k.parse(out)
			require.NoError(t, err)
			assert.Equal(t, v, v2)
			if !inList(fixturesReordered, name) {
				// valid fixtures are written in contract order, so output equals the compacted input
				var compact bytes.Buffer
				require.NoError(t, json.Compact(&compact, fixtureRead(t, name)))
				assert.Equal(t, compact.String(), string(out))
			}
		})
	}
}

func TestInvalidFixturesAreRejected(t *testing.T) {
	for _, name := range fixtureFiles(t, "invalid") {
		t.Run(name, func(t *testing.T) {
			_, err := kinds[strings.SplitN(name, "/", 2)[0]].parse(fixtureRead(t, name))
			require.Error(t, err)
			// json.Unmarshal may report syntax errors itself; the Parse functions always give a *ValidationError
			var ve *mdq.ValidationError
			if strings.HasPrefix(name, "messwert") {
				_, err = mdq.ParseMesswert(fixtureRead(t, name))
			} else {
				_, err = mdq.ParseZaehlerstand(fixtureRead(t, name))
			}
			assert.ErrorAs(t, err, &ve)
		})
	}
}

func TestParseErrorsNameFieldAndRule(t *testing.T) {
	cases := map[string][2]string{
		"messwert/invalid/duplicate_key.json":           {"value", "duplicate"},
		"messwert/invalid/unknown_key.json":             {"", "unknown"},
		"zaehlerstand/invalid/missing_value.json":       {"value", "required"},
		"zaehlerstand/invalid/value_null.json":          {"value", "null"},
		"messwert/invalid/status_gemessen_retired.json": {"status", "wahr"},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := kinds[strings.SplitN(name, "/", 2)[0]].parse(fixtureRead(t, name))
			var ve *mdq.ValidationError
			require.ErrorAs(t, err, &ve)
			if want[0] != "" {
				assert.Equal(t, want[0], ve.Field)
			}
			assert.Contains(t, ve.Rule, want[1])
		})
	}
}

func TestMaLoFixtures(t *testing.T) {
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
	for _, id := range cases.Valid {
		assert.True(t, mdq.IsValidMaLoID(id), id)
	}
	for _, c := range cases.Invalid {
		assert.False(t, mdq.IsValidMaLoID(c.ID), "%q (%s)", c.ID, c.Reason)
	}
	for _, c := range cases.Digits {
		got, err := mdq.MaLoCheckDigit(c.FirstTen)
		require.NoError(t, err, c.FirstTen)
		assert.Equal(t, c.CheckDigit, got, c.FirstTen)
	}
	for _, c := range cases.BadInput {
		_, err := mdq.MaLoCheckDigit(c.FirstTen)
		assert.Error(t, err, "%q (%s)", c.FirstTen, c.Reason)
	}
}

func TestHerkunftMatchesStatusFixture(t *testing.T) {
	var cases []struct {
		Status   string  `json:"status"`
		Herkunft *string `json:"herkunft"`
		OK       bool    `json:"ok"`
	}
	require.NoError(t, json.Unmarshal(fixtureDuplicateFree(t, "herkunft_status.json"), &cases))
	require.GreaterOrEqual(t, len(cases), 12)
	for _, c := range cases {
		var h *mdq.Herkunft
		if c.Herkunft != nil {
			v := mdq.Herkunft(*c.Herkunft)
			h = &v
		}
		assert.Equal(t, c.OK, mdq.HerkunftMatchesStatus(mdq.Status(c.Status), h), "%s/%v", c.Status, c.Herkunft)
	}
}

func TestProtoEnumsFixtureAgreesWithGoEnums(t *testing.T) {
	var enums map[string]struct {
		Values []struct {
			JSON string `json:"json"`
		} `json:"values"`
	}
	require.NoError(t, json.Unmarshal(fixtureDuplicateFree(t, "proto_enums.json"), &enums))
	have := map[string][]string{
		"Status":    {"wahr", "vorlaeufig", "ersatzwert", "fehlend"},
		"Herkunft":  {"fremd", "mqa"},
		"Profiltyp": {"haushalt", "gewerbe", "industrie", "unbekannt"},
		"Richtung":  {"bezug", "einspeisung"},
	}
	validators := map[string]func(string) error{
		"Status":    func(s string) error { return mdq.Status(s).Validate() },
		"Herkunft":  func(s string) error { return mdq.Herkunft(s).Validate() },
		"Profiltyp": func(s string) error { return mdq.Profiltyp(s).Validate() },
		"Richtung":  func(s string) error { return mdq.Richtung(s).Validate() },
	}
	for name, e := range enums {
		var got []string
		for _, v := range e.Values {
			got = append(got, v.JSON)
			assert.NoError(t, validators[name](v.JSON), "%s %s", name, v.JSON)
		}
		assert.Equal(t, have[name], got, name)
		for _, bad := range []string{"", strings.ToUpper(got[0]), "gemessen"} {
			assert.Error(t, validators[name](bad), "%s %q", name, bad)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func validMesswert() mdq.Messwert {
	return mdq.Messwert{
		Malo:       "51238696781",
		Timestamp:  time.Date(2026, 1, 15, 10, 15, 0, 0, time.UTC),
		Resolution: mdq.PT15M,
		Value:      ptr(mdq.Decimal("1.5")),
		Status:     mdq.StatusWahr,
		Herkunft:   ptr(mdq.HerkunftFremd),
		Profiltyp:  mdq.ProfiltypHaushalt,
		Richtung:   mdq.RichtungBezug,
	}
}

func TestMesswertValidateInCode(t *testing.T) {
	require.NoError(t, validMesswert().Validate())
	berlin := time.FixedZone("CET", 3600)
	nepal := time.FixedZone("NPT", 5*3600+45*60)
	tests := map[string]struct {
		mutate func(*mdq.Messwert)
		field  string
	}{
		"bad malo":          {func(m *mdq.Messwert) { m.Malo = "51238696782" }, "malo"},
		"zero time":         {func(m *mdq.Messwert) { m.Timestamp = time.Time{} }, "timestamp"},
		"off raster":        {func(m *mdq.Messwert) { m.Timestamp = m.Timestamp.Add(7 * time.Minute) }, "timestamp"},
		"sub second":        {func(m *mdq.Messwert) { m.Timestamp = m.Timestamp.Add(time.Nanosecond) }, "timestamp"},
		"year 1899":         {func(m *mdq.Messwert) { m.Timestamp = time.Date(1899, 12, 31, 23, 45, 0, 0, time.UTC) }, "timestamp"},
		"year 10000":        {func(m *mdq.Messwert) { m.Timestamp = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }, "timestamp"},
		"resolution":        {func(m *mdq.Messwert) { m.Resolution = "PT5M" }, "resolution"},
		"empty resolution":  {func(m *mdq.Messwert) { m.Resolution = "" }, "resolution"},
		"seven decimals":    {func(m *mdq.Messwert) { m.Value = ptr(mdq.Decimal("0.1234567")) }, "value"},
		"trailing zero":     {func(m *mdq.Messwert) { m.Value = ptr(mdq.Decimal("1.50")) }, "value"},
		"NaN":               {func(m *mdq.Messwert) { m.Value = ptr(mdq.Decimal("NaN")) }, "value"},
		"Infinity":          {func(m *mdq.Messwert) { m.Value = ptr(mdq.Decimal("Infinity")) }, "value"},
		"nil value":         {func(m *mdq.Messwert) { m.Value = nil }, "value"},
		"unknown status":    {func(m *mdq.Messwert) { m.Status = "gemessen" }, "status"},
		"wahr mqa":          {func(m *mdq.Messwert) { m.Herkunft = ptr(mdq.HerkunftMqa) }, "herkunft"},
		"nil herkunft":      {func(m *mdq.Messwert) { m.Herkunft = nil }, "herkunft"},
		"unknown herkunft":  {func(m *mdq.Messwert) { m.Herkunft = ptr(mdq.Herkunft("x")) }, "herkunft"},
		"unknown profiltyp": {func(m *mdq.Messwert) { m.Profiltyp = "" }, "profiltyp"},
		"unknown richtung":  {func(m *mdq.Messwert) { m.Richtung = "Bezug" }, "richtung"},
		"fehlend with value": {func(m *mdq.Messwert) {
			m.Status, m.Herkunft = mdq.StatusFehlend, nil
		}, "value"},
		"fehlend with herkunft": {func(m *mdq.Messwert) {
			m.Status, m.Value = mdq.StatusFehlend, nil
		}, "herkunft"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			m := validMesswert()
			tc.mutate(&m)
			var ve *mdq.ValidationError
			require.ErrorAs(t, m.Validate(), &ve)
			assert.Equal(t, tc.field, ve.Field)
			assert.NotEmpty(t, ve.Rule)
			_, err := json.Marshal(m)
			assert.Error(t, err, "marshal must validate too")
		})
	}

	t.Run("fehlend marshals nulls", func(t *testing.T) {
		m := validMesswert()
		m.Status, m.Value, m.Herkunft = mdq.StatusFehlend, nil, nil
		out, err := json.Marshal(m)
		require.NoError(t, err)
		assert.Equal(t, `{"malo":"51238696781","timestamp":"2026-01-15T10:15:00Z","resolution":"PT15M","value":null,"status":"fehlend","herkunft":null,"profiltyp":"haushalt","richtung":"bezug"}`, string(out))
	})
	t.Run("non-UTC is normalised to UTC", func(t *testing.T) {
		m := validMesswert()
		m.Timestamp = time.Date(2026, 1, 15, 11, 15, 0, 0, berlin)
		out, err := json.Marshal(m)
		require.NoError(t, err)
		assert.Contains(t, string(out), `"timestamp":"2026-01-15T10:15:00Z"`)
		m.Timestamp = time.Date(2026, 1, 15, 16, 0, 0, 0, nepal) // +05:45 is 10:15 UTC
		out, err = json.Marshal(m)
		require.NoError(t, err)
		assert.Contains(t, string(out), `"timestamp":"2026-01-15T10:15:00Z"`)
	})
	t.Run("pointer marshals like value", func(t *testing.T) {
		m := validMesswert()
		a, err := json.Marshal(m)
		require.NoError(t, err)
		b, err := json.Marshal(&m)
		require.NoError(t, err)
		assert.Equal(t, a, b)
	})
}

func TestZaehlerstandValidateInCode(t *testing.T) {
	valid := mdq.Zaehlerstand{
		Malo:      "51238696781",
		Timestamp: time.Date(2026, 1, 15, 10, 17, 23, 0, time.UTC),
		Value:     "12345.678",
	}
	require.NoError(t, valid.Validate())
	out, err := json.Marshal(valid)
	require.NoError(t, err)
	assert.Equal(t, `{"malo":"51238696781","timestamp":"2026-01-15T10:17:23Z","value":"12345.678"}`, string(out))

	tests := map[string]struct {
		mutate func(*mdq.Zaehlerstand)
		field  string
	}{
		"bad malo":       {func(z *mdq.Zaehlerstand) { z.Malo = "1" }, "malo"},
		"zero time":      {func(z *mdq.Zaehlerstand) { z.Timestamp = time.Time{} }, "timestamp"},
		"sub second":     {func(z *mdq.Zaehlerstand) { z.Timestamp = z.Timestamp.Add(time.Millisecond) }, "timestamp"},
		"year 1899":      {func(z *mdq.Zaehlerstand) { z.Timestamp = time.Date(1899, 1, 1, 0, 0, 0, 0, time.UTC) }, "timestamp"},
		"empty value":    {func(z *mdq.Zaehlerstand) { z.Value = "" }, "value"},
		"seven decimals": {func(z *mdq.Zaehlerstand) { z.Value = "0.0000001" }, "value"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			z := valid
			tc.mutate(&z)
			var ve *mdq.ValidationError
			require.ErrorAs(t, z.Validate(), &ve)
			assert.Equal(t, tc.field, ve.Field)
			_, err := json.Marshal(z)
			assert.Error(t, err)
		})
	}
	t.Run("any second and non-UTC zone are fine", func(t *testing.T) {
		z := valid
		z.Timestamp = time.Date(2026, 1, 15, 11, 17, 23, 0, time.FixedZone("CET", 3600))
		out, err := json.Marshal(z)
		require.NoError(t, err)
		assert.Contains(t, string(out), `"timestamp":"2026-01-15T10:17:23Z"`)
	})
}

func TestDecimalValidate(t *testing.T) {
	for _, ok := range []string{"0", "1", "-1", "0.000001", "-0.5", "1000000", "999999999999.999999", "-999999999999.999999", "10", "100.25"} {
		assert.NoError(t, mdq.Decimal(ok).Validate(), ok)
	}
	for _, bad := range []string{"", "1.0", "0.0", "01", "+1", "1e3", "1E3", "-0", "-0.0", ".5", "5.", "NaN", "Infinity", "-Infinity",
		"0.0000001", "0.1234567", "1000000000000", "-1000000000000", "1234567890123456789012345678901234.567891", "1\n", "1_000", "1,5", " 1", "1١", "１"} {
		assert.Error(t, mdq.Decimal(bad).Validate(), "%q", bad)
	}
}

func TestDecimalJSON(t *testing.T) {
	var d mdq.Decimal
	require.NoError(t, json.Unmarshal([]byte(`"1.5"`), &d))
	assert.Equal(t, mdq.Decimal("1.5"), d)
	out, err := json.Marshal(d)
	require.NoError(t, err)
	assert.Equal(t, `"1.5"`, string(out))
	assert.Error(t, json.Unmarshal([]byte(`1.5`), &d), "JSON numbers are rejected")
	assert.Error(t, json.Unmarshal([]byte(`null`), &d))
	assert.Error(t, json.Unmarshal([]byte(`"1.50"`), &d))
	_, err = json.Marshal(mdq.Decimal("1.50"))
	assert.Error(t, err)
}

func TestEnumJSON(t *testing.T) {
	var s mdq.Status
	require.NoError(t, json.Unmarshal([]byte(`"vorlaeufig"`), &s))
	assert.Equal(t, mdq.StatusVorlaeufig, s)
	out, err := json.Marshal(s)
	require.NoError(t, err)
	assert.Equal(t, `"vorlaeufig"`, string(out))
	for _, bad := range []string{`"Wahr"`, `"gemessen"`, `""`, `null`, `1`} {
		assert.Error(t, json.Unmarshal([]byte(bad), &s), bad)
	}
	_, err = json.Marshal(mdq.Status("gemessen"))
	assert.Error(t, err)
	var h mdq.Herkunft
	assert.Error(t, json.Unmarshal([]byte(`"MQA"`), &h))
	var p mdq.Profiltyp
	assert.Error(t, json.Unmarshal([]byte(`"Haushalt"`), &p))
	var r mdq.Richtung
	assert.Error(t, json.Unmarshal([]byte(`"bogus"`), &r))
	assert.Equal(t, mdq.Resolution("PT15M"), mdq.PT15M)
}

func TestParsingAcceptsNothingBeyondOneObject(t *testing.T) {
	var z mdq.Zaehlerstand
	assert.Error(t, json.Unmarshal([]byte(`{"malo":"51238696781","timestamp":"2026-01-15T10:17:23Z","value":"1"} {}`), &z))
	assert.Error(t, json.Unmarshal([]byte(`{"malo":"51238696781","timestamp":"2026-01-15T10:17:23Z","value":"1",}`), &z))
	assert.Error(t, z.UnmarshalJSON([]byte(`{"malo":"51238696781","timestamp":"2026-01-15T10:17:23Z","value":"1"} x`)))
}

func TestNewDecimalNormalisesLikePython(t *testing.T) {
	ok := map[string]string{
		"1.5000000": "1.5", "1.5": "1.5", "2.0": "2", "-0.00": "0", "-0": "0", "0": "0",
		"100": "100", "999999999999.9999990": "999999999999.999999", "100.000": "100", "0.000001": "0.000001", "0.0000010": "0.000001", "-3.50": "-3.5",
	}
	for in, want := range ok {
		got, err := mdq.NewDecimal(in)
		require.NoError(t, err, in)
		assert.Equal(t, mdq.Decimal(want), got, in)
	}
	for _, bad := range []string{"1.0000001", "0.1234567", "1000000000000", "1000000000000.5", "-1234567890123", "", "01", "+1", "1e3", ".5", "5.", "NaN", "Infinity", "1_000", "1\n", "１"} {
		_, err := mdq.NewDecimal(bad)
		var ve *mdq.ValidationError
		assert.ErrorAs(t, err, &ve, "%q", bad)
	}
}

func TestNewDecimalFromFloat(t *testing.T) {
	d, err := mdq.NewDecimal(strconv.FormatFloat(math.Round((0.1+0.2)*1e6)/1e6, 'f', -1, 64))
	require.NoError(t, err)
	assert.Equal(t, mdq.Decimal("0.3"), d)
	for _, x := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := mdq.NewDecimal(strconv.FormatFloat(x, 'f', -1, 64))
		assert.Error(t, err)
	}
}

func TestParseFunctionsReturnValidationErrors(t *testing.T) {
	good := map[string]string{
		"messwert":     string(fixtureRead(t, "messwert/valid/wahr_fremd.json")),
		"zaehlerstand": string(fixtureRead(t, "zaehlerstand/valid/value_zero.json")),
	}
	parse := map[string]func([]byte) error{
		"messwert":     func(b []byte) error { _, err := mdq.ParseMesswert(b); return err },
		"zaehlerstand": func(b []byte) error { _, err := mdq.ParseZaehlerstand(b); return err },
	}
	for k, p := range parse {
		require.NoError(t, p([]byte(good[k])), k)
		for name, in := range map[string]string{
			"null": "null", "array": "[]", "string": `"x"`, "number": "1", "empty": "", "whitespace": " \n",
			"syntax error": `{"malo": `, "unclosed": `{"malo":"51238696781"`, "garbage": `{x}`,
			"trailing data": good[k] + " {}", "trailing garbage": good[k] + " x", "trailing comma": strings.Replace(good[k], "}", ",}", 1),
			"bom": "\xef\xbb\xbf" + good[k], "invalid utf8": "\xff", "utf16": "\xff\xfe{\x00}\x00",
		} {
			err := p([]byte(in))
			var ve *mdq.ValidationError
			assert.ErrorAs(t, err, &ve, "%s %s: %v", k, name, err)
		}
	}
	// the caveat documented on UnmarshalJSON: encoding/json does not reject a top-level null into a pointer
	var p *mdq.Messwert
	require.NoError(t, json.Unmarshal([]byte("null"), &p))
	assert.Nil(t, p)
	var v mdq.Messwert
	assert.Error(t, json.Unmarshal([]byte("null"), &v))
	var se *json.SyntaxError
	assert.ErrorAs(t, json.Unmarshal([]byte(`{`), &v), &se, "json.Unmarshal reports syntax errors itself")
}

func TestParseFunctionsMatchFixtures(t *testing.T) {
	for _, name := range fixtureFiles(t, "valid") {
		var err error
		if strings.HasPrefix(name, "messwert") {
			_, err = mdq.ParseMesswert(fixtureRead(t, name))
		} else {
			_, err = mdq.ParseZaehlerstand(fixtureRead(t, name))
		}
		assert.NoError(t, err, name)
	}
}

func TestHugeJSONNumberIsRejectedForTheRightReason(t *testing.T) {
	for _, name := range []string{"messwert/invalid/value_huge_json_number.json", "zaehlerstand/invalid/value_huge_json_number.json"} {
		var err error
		if strings.HasPrefix(name, "messwert") {
			_, err = mdq.ParseMesswert(fixtureRead(t, name))
		} else {
			_, err = mdq.ParseZaehlerstand(fixtureRead(t, name))
		}
		var ve *mdq.ValidationError
		require.ErrorAs(t, err, &ve, name)
		assert.Equal(t, "value", ve.Field, name)
		assert.Contains(t, ve.Rule, "string", name)
		assert.NotContains(t, ve.Rule, "duplicate", name)
	}
}

func TestResolutionJSON(t *testing.T) {
	var r mdq.Resolution
	require.NoError(t, json.Unmarshal([]byte(`"PT15M"`), &r))
	assert.Equal(t, mdq.PT15M, r)
	out, err := json.Marshal(r)
	require.NoError(t, err)
	assert.Equal(t, `"PT15M"`, string(out))
	for _, bad := range []string{`"PT5M"`, `"PT1H"`, `"15"`, `""`, `null`, `15`} {
		assert.Error(t, json.Unmarshal([]byte(bad), &r), bad)
	}
	assert.Equal(t, mdq.PT15M, r, "failed unmarshal leaves the value unchanged")
	_, err = json.Marshal(mdq.Resolution("PT5M"))
	assert.Error(t, err)
	_, err = json.Marshal(mdq.Resolution(""))
	assert.Error(t, err)
}
