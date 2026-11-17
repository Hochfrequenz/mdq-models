package mdqmodels

import (
	"encoding/json"
	"regexp"
	"time"
)

var einflusswertKeys = []string{"malo", "timestamp", "resolution", "groesse", "value", "quelle"}

// quellePattern is the shape of Einflusswert.Quelle: 1 to 64 printable ASCII characters, no space.
// Go's $ matches only at the end of the text, so a trailing newline is rejected.
var quellePattern = regexp.MustCompile("^[!-~]{1,64}$")

// Einflusswert is one row of an influence series: an influence value (for example the air
// temperature) for one MaLo and one period.
//
// The period is [Timestamp, Timestamp + Resolution): Timestamp is its START, as for Messwert.
// All six JSON keys are required. Value is required-nullable: it is always written, as null when
// missing (nil here), and a missing key is rejected on read. A Value of 0 is a value, not a gap.
// Parsing is strict on purpose: unknown and duplicate keys are rejected.
type Einflusswert struct {
	// Malo is the market location ID: 11 ASCII digits, no leading 0, valid check digit.
	Malo string
	// Timestamp is the START of the period [Timestamp, Timestamp + Resolution), on the quarter-hour
	// raster, and on the full hour for EinflussPT1H. The raster is checked on the UTC instant. In code
	// any time zone is accepted and written in UTC.
	Timestamp time.Time
	// Resolution is the raster of the series: EinflussPT1H or EinflussPT15M.
	Resolution EinflussResolution
	// Groesse is the measured quantity; its unit is fixed per Groesse.
	Groesse Groesse
	// Value is the value, or nil if it is missing. Negative values are allowed.
	Value *Decimal
	// Quelle is the source of the value: 1 to 64 printable ASCII characters without spaces, for
	// example "dwd:01993".
	Quelle string
}

// Validate checks every rule of the contract on a value built in code and returns a
// *ValidationError naming the first broken field.
func (e Einflusswert) Validate() error {
	if err := checkMaLo(e.Malo); err != nil {
		return inField("malo", err)
	}
	if err := checkTimestamp(e.Timestamp, true); err != nil {
		return inField("timestamp", err)
	}
	if err := e.Resolution.Validate(); err != nil {
		return inField("resolution", err)
	}
	if e.Resolution == EinflussPT1H && e.Timestamp.UTC().Minute() != 0 {
		return &ValidationError{Field: "timestamp", Rule: "must be on the full hour (UTC) for resolution \"PT1H\""}
	}
	if err := e.Groesse.Validate(); err != nil {
		return inField("groesse", err)
	}
	if e.Value != nil {
		if err := e.Value.Validate(); err != nil {
			return inField("value", err)
		}
	}
	if !quellePattern.MatchString(e.Quelle) {
		return &ValidationError{Field: "quelle", Rule: "must be 1 to 64 printable ASCII characters without spaces"}
	}
	return nil
}

// einflusswertWire fixes the key order of the output: the field order here is the order in the
// golden file. Do not reorder.
type einflusswertWire struct {
	Malo       string  `json:"malo"`
	Timestamp  string  `json:"timestamp"`
	Resolution string  `json:"resolution"`
	Groesse    string  `json:"groesse"`
	Value      *string `json:"value"`
	Quelle     string  `json:"quelle"`
}

// MarshalJSON validates e and writes it with the keys in contract order and the timestamp in UTC
// with a literal Z; a missing Value is written as null. It has a value receiver so that both
// Einflusswert and *Einflusswert marshal the same way.
func (e Einflusswert) MarshalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	w := einflusswertWire{
		Malo:       e.Malo,
		Timestamp:  formatTimestamp(e.Timestamp),
		Resolution: string(e.Resolution),
		Groesse:    string(e.Groesse),
		Quelle:     e.Quelle,
	}
	if e.Value != nil {
		s := string(*e.Value)
		w.Value = &s
	}
	return json.Marshal(w)
}

// ParseEinflusswert reads one Einflusswert from JSON bytes. It is the recommended entry point:
// every failure, including null, a non-object, a syntax error, trailing data, a byte order mark and
// invalid UTF-8, is a *ValidationError. The strictness is that of UnmarshalJSON.
func ParseEinflusswert(b []byte) (Einflusswert, error) {
	var e Einflusswert
	if err := e.UnmarshalJSON(b); err != nil {
		return Einflusswert{}, err
	}
	return e, nil
}

// UnmarshalJSON reads one Einflusswert strictly: all six keys required (in any order), no unknown or
// duplicate keys, timestamps exactly in the form YYYY-MM-DDTHH:MM:SSZ, and every rule of Validate.
// On error the receiver is left unchanged.
//
// Called through json.Unmarshal, syntax errors surface as *json.SyntaxError (not a *ValidationError),
// and a top-level null into a *Einflusswert pointer (var p *Einflusswert) sets the pointer to nil
// without any error, because encoding/json never calls UnmarshalJSON for it. Use ParseEinflusswert
// for untrusted input.
func (e *Einflusswert) UnmarshalJSON(b []byte) error {
	fields, err := decodeObject(b, einflusswertKeys)
	if err != nil {
		return err
	}
	var out Einflusswert
	if out.Malo, err = stringField(fields, "malo"); err != nil {
		return err
	}
	if out.Timestamp, err = timestampField(fields, "timestamp"); err != nil {
		return err
	}
	if err = readEinflusswertScalars(fields, &out); err != nil {
		return err
	}
	if err = out.Validate(); err != nil {
		return err
	}
	*e = out
	return nil
}

func readEinflusswertScalars(fields map[string]json.RawMessage, out *Einflusswert) error {
	resolution, err := stringField(fields, "resolution")
	if err != nil {
		return err
	}
	out.Resolution = EinflussResolution(resolution)
	groesse, err := stringField(fields, "groesse")
	if err != nil {
		return err
	}
	out.Groesse = Groesse(groesse)
	value, err := nullableStringField(fields, "value")
	if err != nil {
		return err
	}
	if value != nil {
		d := Decimal(*value)
		out.Value = &d
	}
	out.Quelle, err = stringField(fields, "quelle")
	return err
}
