package mdqmodels

import (
	"encoding/json"
	"time"
)

var zaehlerstandKeys = []string{"malo", "timestamp", "value"}

// Zaehlerstand is a meter reading: the cumulative register value of one MaLo at one point in time.
// It is the second input of the schema, next to Messwert.
//
// All three JSON keys are required and parsing is strict like for Messwert (unknown and duplicate
// keys are rejected). Unlike Messwert there is no raster: Timestamp is a point in time with any whole
// second, not the start of a quarter hour, and Value is never null.
type Zaehlerstand struct {
	// Malo is the market location ID: 11 ASCII digits, no leading 0, valid check digit.
	Malo string
	// Timestamp is the instant of the reading, a whole second. In code any time zone is accepted and
	// written in UTC.
	Timestamp time.Time
	// Value is the register value; required, never null.
	Value Decimal
}

// Validate checks every rule of the contract on a value built in code and returns a
// *ValidationError naming the first broken field.
func (z Zaehlerstand) Validate() error {
	if err := checkMaLo(z.Malo); err != nil {
		return inField("malo", err)
	}
	if err := checkTimestamp(z.Timestamp, false); err != nil {
		return inField("timestamp", err)
	}
	return inField("value", z.Value.Validate())
}

// zaehlerstandWire fixes the key order of the output. Do not reorder.
type zaehlerstandWire struct {
	Malo      string `json:"malo"`
	Timestamp string `json:"timestamp"`
	Value     string `json:"value"`
}

// MarshalJSON validates z and writes it with the keys in contract order and the timestamp in UTC
// with a literal Z.
func (z Zaehlerstand) MarshalJSON() ([]byte, error) {
	if err := z.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(zaehlerstandWire{Malo: z.Malo, Timestamp: formatTimestamp(z.Timestamp), Value: string(z.Value)})
}

// ParseZaehlerstand reads one Zaehlerstand from JSON bytes. It is the recommended entry point: every
// failure, including null, a non-object, a syntax error, trailing data, a byte order mark and
// invalid UTF-8, is a *ValidationError. The strictness is that of UnmarshalJSON.
func ParseZaehlerstand(b []byte) (Zaehlerstand, error) {
	var z Zaehlerstand
	if err := z.UnmarshalJSON(b); err != nil {
		return Zaehlerstand{}, err
	}
	return z, nil
}

// UnmarshalJSON reads one Zaehlerstand strictly: all three keys required (in any order), no unknown
// or duplicate keys, and every rule of Validate. On error the receiver is left unchanged.
//
// Called through json.Unmarshal, syntax errors surface as *json.SyntaxError (not a *ValidationError),
// and a top-level null into a *Zaehlerstand pointer (var p *Zaehlerstand) sets the pointer to nil
// without any error, because encoding/json never calls UnmarshalJSON for it. Use ParseZaehlerstand
// for untrusted input.
func (z *Zaehlerstand) UnmarshalJSON(b []byte) error {
	fields, err := decodeObject(b, zaehlerstandKeys)
	if err != nil {
		return err
	}
	var out Zaehlerstand
	if out.Malo, err = stringField(fields, "malo"); err != nil {
		return err
	}
	if out.Timestamp, err = timestampField(fields, "timestamp"); err != nil {
		return err
	}
	value, err := stringField(fields, "value")
	if err != nil {
		return err
	}
	out.Value = Decimal(value)
	if err = out.Validate(); err != nil {
		return err
	}
	*z = out
	return nil
}
