package mdqmodels_test

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	mdq "github.com/hochfrequenz/mdq-models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ExampleParseMesswert is the Go snippet of the README; TestReadmeGoSnippet keeps the two identical.
func ExampleParseMesswert() {
	raw := []byte(`{"malo":"68283561554","timestamp":"2026-03-29T00:45:00Z","resolution":"PT15M",` +
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
		Malo:       "68283561554",
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
	_, err = mdq.ParseMesswert([]byte(`{"malo":"68283561554","malo":"68283561554"}`))
	var invalid *mdq.ValidationError
	fmt.Println(errors.As(err, &invalid))
	// Output:
	// 68283561554 2026-03-29T00:45:00Z 1.5 wahr
	// {"malo":"68283561554","timestamp":"2026-03-29T01:00:00Z","resolution":"PT15M","value":"2.5","status":"ersatzwert","herkunft":"mqa","profiltyp":"haushalt","richtung":"bezug"}
	// true
}

// TestReadmeGoSnippet fails when the Go snippet in README.md is not the body of ExampleParseMesswert.
func TestReadmeGoSnippet(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	require.NoError(t, err)
	source, err := os.ReadFile("example_test.go")
	require.NoError(t, err)

	_, afterFence, found := strings.Cut(string(readme), "```go\n")
	require.True(t, found, "README.md has no go snippet")
	snippet, _, _ := strings.Cut(afterFence, "```\n")

	_, afterFunc, found := strings.Cut(string(source), "func ExampleParseMesswert() {\n")
	require.True(t, found)
	body, _, _ := strings.Cut(afterFunc, "\n}\n")
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "\t") // the function body is indented one level
	}
	assert.Equal(t, strings.Join(lines, "\n"), strings.TrimSuffix(snippet, "\n"))
}
