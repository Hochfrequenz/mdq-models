package mdqmodels_test

import (
	"encoding/json"
	"testing"
	"time"

	mdq "github.com/hochfrequenz/mdq-models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validEinflusswert() mdq.Einflusswert {
	return mdq.Einflusswert{
		Malo:       "68283561554",
		Timestamp:  time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		Resolution: mdq.EinflussPT1H,
		Groesse:    mdq.GroesseTemperatur,
		Value:      ptr(mdq.Decimal("-3.5")),
		Quelle:     "dwd:01993",
	}
}

func TestEinflusswertMarshalKeyOrder(t *testing.T) {
	raw, err := json.Marshal(validEinflusswert())
	require.NoError(t, err)
	assert.Equal(t, `{"malo":"68283561554","timestamp":"2026-01-15T10:00:00Z","resolution":"PT1H",`+
		`"groesse":"temperatur","value":"-3.5","quelle":"dwd:01993"}`, string(raw))
	parsed, err := mdq.ParseEinflusswert(raw)
	require.NoError(t, err)
	assert.Equal(t, validEinflusswert(), parsed)
}

func TestEinflusswertNilValueIsNull(t *testing.T) {
	e := validEinflusswert()
	e.Value = nil
	raw, err := json.Marshal(e)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"value":null`)
	parsed, err := mdq.ParseEinflusswert(raw)
	require.NoError(t, err)
	assert.Nil(t, parsed.Value)

	_, err = mdq.ParseEinflusswert([]byte(`{"malo":"68283561554","timestamp":"2026-01-15T10:00:00Z",` +
		`"resolution":"PT1H","groesse":"temperatur","quelle":"dwd:01993"}`))
	var ve *mdq.ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Equal(t, "value", ve.Field)
}

func TestEinflusswertValueZeroStaysZero(t *testing.T) {
	e := validEinflusswert()
	e.Value = ptr(mdq.Decimal("0"))
	raw, err := json.Marshal(e)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"value":"0"`)
}

func TestEinflusswertRasterPerResolution(t *testing.T) {
	at := func(res mdq.EinflussResolution, ts time.Time) error {
		e := validEinflusswert()
		e.Resolution, e.Timestamp = res, ts
		return e.Validate()
	}
	utc := func(h, m int) time.Time { return time.Date(2026, 1, 15, h, m, 0, 0, time.UTC) }

	var ve *mdq.ValidationError
	require.ErrorAs(t, at(mdq.EinflussPT1H, utc(10, 15)), &ve)
	assert.Equal(t, "timestamp", ve.Field)
	assert.NoError(t, at(mdq.EinflussPT15M, utc(10, 15)))
	assert.Error(t, at(mdq.EinflussPT15M, utc(10, 7)))

	ist := time.Date(2026, 1, 15, 16, 0, 0, 0, time.FixedZone("IST", 5*3600+1800))
	require.ErrorAs(t, at(mdq.EinflussPT1H, ist), &ve)
	assert.Equal(t, "timestamp", ve.Field)
	e := validEinflusswert()
	e.Resolution, e.Timestamp = mdq.EinflussPT15M, ist
	raw, err := json.Marshal(e)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"timestamp":"2026-01-15T10:30:00Z"`)
}

func TestEinflusswertDSTHours(t *testing.T) {
	for _, c := range []struct {
		ts   time.Time
		want string
	}{
		{time.Date(2026, 10, 25, 2, 0, 0, 0, time.FixedZone("CEST", 2*3600)), "2026-10-25T00:00:00Z"},
		{time.Date(2026, 10, 25, 2, 0, 0, 0, time.FixedZone("CET", 3600)), "2026-10-25T01:00:00Z"},
		{time.Date(2026, 3, 29, 3, 0, 0, 0, time.FixedZone("CEST", 2*3600)), "2026-03-29T01:00:00Z"},
	} {
		e := validEinflusswert()
		e.Timestamp = c.ts
		raw, err := json.Marshal(e)
		require.NoError(t, err)
		assert.Contains(t, string(raw), `"timestamp":"`+c.want+`"`)
	}
}

func TestEinflusswertQuelle(t *testing.T) {
	long := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = 'x'
		}
		return string(b)
	}
	for _, ok := range []string{"dwd:01993", "a", long(64), "~", "!"} {
		e := validEinflusswert()
		e.Quelle = ok
		assert.NoError(t, e.Validate(), ok)
	}
	for _, bad := range []string{"", "dwd: 01993", "dwd:01993 ", "dwd:München", long(65), "dwd:01993\n", "a\tb", "\x7f"} {
		e := validEinflusswert()
		e.Quelle = bad
		var ve *mdq.ValidationError
		require.ErrorAs(t, e.Validate(), &ve, "%q", bad)
		assert.Equal(t, "quelle", ve.Field)
	}
}

func TestEinflusswertInvalidDoesNotMarshal(t *testing.T) {
	e := validEinflusswert()
	e.Groesse = "cloud_cover"
	_, err := json.Marshal(e)
	assert.Error(t, err)
}

func TestEinflusswertParseIsStrict(t *testing.T) {
	good := `{"malo":"68283561554","timestamp":"2026-01-15T10:00:00Z","resolution":"PT1H",` +
		`"groesse":"temperatur","value":"-3.5","quelle":"dwd:01993"}`
	_, err := mdq.ParseEinflusswert([]byte(good))
	require.NoError(t, err)
	for _, bad := range []string{
		`{"malo":"68283561554","timestamp":"2026-01-15T10:00:00Z","resolution":"pt1h","groesse":"temperatur","value":null,"quelle":"q"}`,
		`{"malo":"68283561554","timestamp":"2026-01-15T10:00:00Z","resolution":"PT1H","groesse":"Temperatur","value":null,"quelle":"q"}`,
		`{"malo":"68283561554","timestamp":"2026-01-15T10:00:00Z","resolution":"PT1H","groesse":"temperatur","value":null,"quelle":"q","einheit":"C"}`,
		`{"malo":"68283561554","timestamp":"2026-01-15T10:00:00Z","resolution":"PT1H","groesse":"temperatur","value":null,"quelle":"q","quelle":"r"}`,
		`{"malo":"68283561554","timestamp":"2026-01-15T10:15:00Z","resolution":"PT1H","groesse":"temperatur","value":null,"quelle":"q"}`,
		`null`,
	} {
		_, err := mdq.ParseEinflusswert([]byte(bad))
		var ve *mdq.ValidationError
		assert.ErrorAs(t, err, &ve, bad)
	}
}

func TestEinflussResolutionJSONRoundTrip(t *testing.T) {
	for _, r := range []mdq.EinflussResolution{mdq.EinflussPT1H, mdq.EinflussPT15M} {
		raw, err := json.Marshal(r)
		require.NoError(t, err)
		assert.Equal(t, `"`+string(r)+`"`, string(raw))
		var back mdq.EinflussResolution
		require.NoError(t, json.Unmarshal(raw, &back))
		assert.Equal(t, r, back)
	}
	var r mdq.EinflussResolution
	assert.Error(t, json.Unmarshal([]byte(`"pt1h"`), &r))
	assert.Error(t, r.UnmarshalJSON([]byte(`null`)))
	_, err := json.Marshal(mdq.EinflussResolution("pt1h"))
	assert.Error(t, err)
}
