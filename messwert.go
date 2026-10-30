package mdqmodels

import (
	"encoding/json"
	"time"
)

var messwertKeys = []string{"malo", "timestamp", "resolution", "value", "status", "herkunft", "profiltyp", "richtung"}

// Messwert is one row of the canonical meter-reading schema: the quantity of one MaLo for one quarter
// hour.
//
// All eight JSON keys are required. Value and Herkunft are required-nullable: they are always
// written, as null when unset (nil here), and a missing key is rejected on read even though null
// would be fine. Parsing is strict on purpose: unknown and duplicate keys are rejected.
type Messwert struct {
	// Malo is the market location ID: 11 ASCII digits, no leading 0, valid check digit.
	Malo string
	// Timestamp is the START of the quarter hour: 00:00 is the first quarter hour of a day and 23:45
	// the last, and the value equals the start of a [from, to) interval, so intervals never overlap
	// and never leave a gap. In code any time zone is accepted and written in UTC.
	Timestamp time.Time
	// Resolution is the raster; only PT15M in v1.
	Resolution Resolution
	// Value is the quantity, or nil if and only if Status is StatusFehlend. Negative values are
	// allowed: the consumer flags them, the schema does not reject them.
	Value *Decimal
	// Status says how trustworthy the value is.
	Status Status
	// Herkunft is the origin of the value, or nil if and only if Status is StatusFehlend;
	// StatusWahr requires HerkunftFremd.
	Herkunft *Herkunft
	// Profiltyp is the load profile kind.
	Profiltyp Profiltyp
	// Richtung is the flow direction.
	Richtung Richtung
}

// Validate checks every rule of the contract on a value built in code and returns a
// *ValidationError naming the first broken field.
func (m Messwert) Validate() error {
	if err := checkMaLo(m.Malo); err != nil {
		return inField("malo", err)
	}
	if err := checkTimestamp(m.Timestamp, true); err != nil {
		return inField("timestamp", err)
	}
	if err := m.Resolution.Validate(); err != nil {
		return inField("resolution", err)
	}
	if err := m.Status.Validate(); err != nil {
		return inField("status", err)
	}
	if err := m.validateValueAndHerkunft(); err != nil {
		return err
	}
	if err := m.Profiltyp.Validate(); err != nil {
		return inField("profiltyp", err)
	}
	return inField("richtung", m.Richtung.Validate())
}

// validateValueAndHerkunft holds the rules that tie value and herkunft to status. The null rule is
// two-sided on purpose: a gap must say so (fehlend, nothing else set), and a value that is not
// missing must say what it is and where it comes from.
func (m Messwert) validateValueAndHerkunft() error {
	missing := m.Status == StatusFehlend
	switch {
	case m.Value == nil && !missing:
		return &ValidationError{Field: "value", Rule: "must not be null unless status is \"fehlend\""}
	case m.Value != nil && missing:
		return &ValidationError{Field: "value", Rule: "must be null when status is \"fehlend\""}
	case m.Value != nil:
		if err := m.Value.Validate(); err != nil {
			return inField("value", err)
		}
	}
	switch {
	case m.Herkunft == nil && !missing:
		return &ValidationError{Field: "herkunft", Rule: "must not be null unless status is \"fehlend\""}
	case m.Herkunft != nil && missing:
		return &ValidationError{Field: "herkunft", Rule: "must be null when status is \"fehlend\""}
	case m.Herkunft != nil:
		if err := m.Herkunft.Validate(); err != nil {
			return inField("herkunft", err)
		}
		if !HerkunftMatchesStatus(m.Status, m.Herkunft) {
			return &ValidationError{Field: "herkunft", Rule: "status \"wahr\" requires herkunft \"fremd\""}
		}
	}
	return nil
}

// messwertWire fixes the key order of the output: the field order here is the order in the golden
// file. Do not reorder.
type messwertWire struct {
	Malo       string  `json:"malo"`
	Timestamp  string  `json:"timestamp"`
	Resolution string  `json:"resolution"`
	Value      *string `json:"value"`
	Status     string  `json:"status"`
	Herkunft   *string `json:"herkunft"`
	Profiltyp  string  `json:"profiltyp"`
	Richtung   string  `json:"richtung"`
}

// MarshalJSON validates m and writes it with the keys in contract order and the timestamp in UTC
// with a literal Z; unset Value and Herkunft are written as null. It has a value receiver so that
// both Messwert and *Messwert marshal the same way.
func (m Messwert) MarshalJSON() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	w := messwertWire{
		Malo:       m.Malo,
		Timestamp:  formatTimestamp(m.Timestamp),
		Resolution: string(m.Resolution),
		Status:     string(m.Status),
		Profiltyp:  string(m.Profiltyp),
		Richtung:   string(m.Richtung),
	}
	if m.Value != nil {
		s := string(*m.Value)
		w.Value = &s
	}
	if m.Herkunft != nil {
		s := string(*m.Herkunft)
		w.Herkunft = &s
	}
	return json.Marshal(w)
}

// ParseMesswert reads one Messwert from JSON bytes. It is the recommended entry point: every
// failure, including null, a non-object, a syntax error, trailing data, a byte order mark and
// invalid UTF-8, is a *ValidationError. The strictness is that of UnmarshalJSON.
func ParseMesswert(b []byte) (Messwert, error) {
	var m Messwert
	if err := m.UnmarshalJSON(b); err != nil {
		return Messwert{}, err
	}
	return m, nil
}

// UnmarshalJSON reads one Messwert strictly: all eight keys required (in any order), no unknown or
// duplicate keys, timestamps exactly in the form YYYY-MM-DDTHH:MM:SSZ, and every rule of Validate.
// On error the receiver is left unchanged.
//
// Called through json.Unmarshal, syntax errors surface as *json.SyntaxError (not a *ValidationError),
// and a top-level null into a *Messwert pointer (var p *Messwert) sets the pointer to nil without
// any error, because encoding/json never calls UnmarshalJSON for it; plan 0.1 rejects top-level
// null. Use ParseMesswert for untrusted input.
func (m *Messwert) UnmarshalJSON(b []byte) error {
	fields, err := decodeObject(b, messwertKeys)
	if err != nil {
		return err
	}
	var out Messwert
	if out.Malo, err = stringField(fields, "malo"); err != nil {
		return err
	}
	if out.Timestamp, err = timestampField(fields, "timestamp"); err != nil {
		return err
	}
	if err = readMesswertScalars(fields, &out); err != nil {
		return err
	}
	if err = out.Validate(); err != nil {
		return err
	}
	*m = out
	return nil
}

func readMesswertScalars(fields map[string]json.RawMessage, out *Messwert) error {
	resolution, err := stringField(fields, "resolution")
	if err != nil {
		return err
	}
	out.Resolution = Resolution(resolution)
	value, err := nullableStringField(fields, "value")
	if err != nil {
		return err
	}
	if value != nil {
		d := Decimal(*value)
		out.Value = &d
	}
	status, err := stringField(fields, "status")
	if err != nil {
		return err
	}
	out.Status = Status(status)
	herkunft, err := nullableStringField(fields, "herkunft")
	if err != nil {
		return err
	}
	if herkunft != nil {
		h := Herkunft(*herkunft)
		out.Herkunft = &h
	}
	profiltyp, err := stringField(fields, "profiltyp")
	if err != nil {
		return err
	}
	out.Profiltyp = Profiltyp(profiltyp)
	richtung, err := stringField(fields, "richtung")
	out.Richtung = Richtung(richtung)
	return err
}
