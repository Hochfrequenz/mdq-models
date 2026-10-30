package mdqmodels

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Resolution is the time raster of a Messwert, written as an ISO 8601 duration.
type Resolution string

// PT15M is the only resolution in v1: one value per quarter hour. It is a field of its own (and not
// implied) so that another raster can be added later without changing the shape of the data.
const PT15M Resolution = "PT15M"

// MarshalJSON writes r as a JSON string after validating it.
func (r Resolution) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(string(r))
}

// UnmarshalJSON reads a JSON string and validates it; null is rejected.
func (r *Resolution) UnmarshalJSON(b []byte) error {
	s, err := unmarshalString(b)
	if err != nil {
		return err
	}
	v := Resolution(s)
	if err := v.Validate(); err != nil {
		return err
	}
	*r = v
	return nil
}

// Validate checks that r is a resolution known to v1.
func (r Resolution) Validate() error {
	if r != PT15M {
		return ruleError("must be %q, got %q", PT15M, string(r))
	}
	return nil
}

// decimalPattern is the shortest canonical decimal with at most 6 digits after the point.
//
// Why 6: it is the precision the consumers agree on. Below 10^9 such a value has at most 15
// significant digits, so it survives a round trip through float64 via the shortest representation;
// everything finer than a micro-unit is noise.
// The limit is counted on the canonical form, so trailing zeros never count ("1.5000000" is not
// canonical at all). Why the canonical form is the only form: 1.50 and 1.5 are one number, and a
// single spelling keeps the golden bytes of Go and Python identical and makes string comparison safe.
//
// Why at most 12 integer digits (|value| < 10^12): a common storage type for such values is a
// numeric(18,6) column, which holds 12 digits before and 6 after the point; a larger value would
// otherwise pass validation and only fail when it is written.
//
// Go's $ matches only at the very end of the text (unlike Python's), and \d is not used, so only
// ASCII digits match and "1\n" is rejected.
var decimalPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,11})(\.[0-9]{0,5}[1-9])?$`)

// Decimal is a decimal number in its shortest canonical string form with at most 12 integer digits
// (|value| < 10^12) and at most 6 decimal places,
// for example "0", "160", "0.000001", "25.25" or "-3.5".
//
// It is a string on the wire so that no consumer loses precision by parsing it as a float. The
// accepted syntax is ^-?(0|[1-9][0-9]*)(\.[0-9]{0,5}[1-9])?$: no exponent, no plus sign, no leading
// zeros, no trailing zeros after the point ("1.0" is rejected), no negative zero ("-0"), no "NaN" or
// "Infinity", no separators. A value with more than 6 decimals ("0.1234567") is rejected, never
// rounded, and so is a value of 10^12 or more (the storage column is numeric(18,6)): producers that compute in floating point round to 6 decimals first. Negative values are
// allowed; the consumer flags them.
//
// Converting a string directly (Decimal("1.50")) does not normalise: Validate then rejects it. Use
// NewDecimal to get the canonical form from "1.50" or "1.5000000". To build one from a float64,
// round to 6 decimals first and format the shortest representation:
//
//	d, err := NewDecimal(strconv.FormatFloat(math.Round(x*1e6)/1e6, 'f', -1, 64))
//
// NaN and the infinities fail in NewDecimal.
type Decimal string

// Validate checks that d is a canonical decimal with at most 12 integer digits and at most 6 decimal
// places.
func (d Decimal) Validate() error {
	s := string(d)
	if !decimalPattern.MatchString(s) {
		if decimalInputPattern.MatchString(s) {
			if intDigits := len(strings.TrimPrefix(strings.SplitN(s, ".", 2)[0], "-")); intDigits > 12 {
				return ruleError("%q has %d integer digits, at most 12 are allowed (value must be below 10^12)", s, intDigits)
			}
		}
		return ruleError("%q is not a canonical decimal string with at most 12 integer digits and 6 decimal places "+
			"(no exponent, plus sign, leading zeros, trailing zeros or separators)", s)
	}
	// "-0" matches the pattern; it is the same number as "0" and would make two spellings of zero.
	if strings.HasPrefix(s, "-") && strings.Trim(s, "-0") == "" {
		return ruleError("%q: negative zero is not allowed, write \"0\"", s)
	}
	return nil
}

var decimalInputPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

// NewDecimal builds a canonical Decimal from s (at most 12 integer digits, see Decimal), which may carry trailing zeros: "1.5000000" becomes
// "1.5", "2.0" becomes "2" and "-0.00" becomes "0". Why: the Python models normalise a decimal built
// in code the same way, so both languages accept and write the same values. Decimals are counted
// after the trailing zeros are removed, so "1.0000001" (7 decimals) is rejected, never rounded.
// Anything else non-canonical (exponent, plus sign, leading zeros, ".5", "5.", NaN, separators) is
// rejected.
func NewDecimal(s string) (Decimal, error) {
	if !decimalInputPattern.MatchString(s) {
		return "", ruleError("%q is not a decimal string (no exponent, plus sign, leading zeros or separators)", s)
	}
	if strings.Contains(s, ".") {
		s = strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		s = "0"
	}
	d := Decimal(s)
	if err := d.Validate(); err != nil {
		return "", err
	}
	return d, nil
}

// MarshalJSON writes d as a JSON string after validating it.
func (d Decimal) MarshalJSON() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(string(d))
}

// UnmarshalJSON reads a JSON string and validates it. JSON numbers and null are rejected: a number
// would be parsed as a float by most consumers and lose precision.
func (d *Decimal) UnmarshalJSON(b []byte) error {
	s, err := unmarshalString(b)
	if err != nil {
		return err
	}
	v := Decimal(s)
	if err := v.Validate(); err != nil {
		return err
	}
	*d = v
	return nil
}

// unmarshalString decodes a JSON string and rejects everything else, null included. (encoding/json
// would silently turn null into "".)
func unmarshalString(b []byte) (string, error) {
	if isNull(b) {
		return "", ruleError("must be a string, not null")
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return "", ruleError("must be a JSON string")
	}
	return s, nil
}

func isNull(b []byte) bool { return strings.TrimSpace(string(b)) == "null" }

// Status says how trustworthy a Messwert is.
type Status string

// The values of Status. The JSON value is the constant's name without the prefix, lower-case, and
// matches the proto enum constants (STATUS_WAHR and so on).
const (
	// StatusWahr is a measured value.
	StatusWahr Status = "wahr"
	// StatusVorlaeufig is a provisional value that may still change.
	StatusVorlaeufig Status = "vorlaeufig"
	// StatusErsatzwert is a substitute value that replaces a missing or implausible measurement.
	StatusErsatzwert Status = "ersatzwert"
	// StatusFehlend marks a missing value; value and herkunft are null.
	StatusFehlend Status = "fehlend"
)

var statusValues = []Status{StatusWahr, StatusVorlaeufig, StatusErsatzwert, StatusFehlend}

// Herkunft says where a Messwert comes from.
type Herkunft string

// The values of Herkunft.
const (
	// HerkunftFremd is a value that was not formed by the meter data quality agent (MQA), for example
	// one delivered from the source system.
	HerkunftFremd Herkunft = "fremd"
	// HerkunftMqa is a value formed by the meter data quality agent (MQA, German "Messdatenqualitaetsagent"),
	// the software that forms provisional and substitute values, in an earlier run.
	HerkunftMqa Herkunft = "mqa"
)

var herkunftValues = []Herkunft{HerkunftFremd, HerkunftMqa}

// Profiltyp is the kind of load profile of a Messwert's MaLo.
type Profiltyp string

// The values of Profiltyp.
const (
	// ProfiltypHaushalt is a household profile.
	ProfiltypHaushalt Profiltyp = "haushalt"
	// ProfiltypGewerbe is a commercial profile.
	ProfiltypGewerbe Profiltyp = "gewerbe"
	// ProfiltypIndustrie is an industrial profile.
	ProfiltypIndustrie Profiltyp = "industrie"
	// ProfiltypUnbekannt is an unknown profile.
	ProfiltypUnbekannt Profiltyp = "unbekannt"
)

var profiltypValues = []Profiltyp{ProfiltypHaushalt, ProfiltypGewerbe, ProfiltypIndustrie, ProfiltypUnbekannt}

// Richtung is the flow direction of a Messwert.
type Richtung string

// The values of Richtung.
const (
	// RichtungBezug is consumption from the grid.
	RichtungBezug Richtung = "bezug"
	// RichtungEinspeisung is feed-in to the grid.
	RichtungEinspeisung Richtung = "einspeisung"
)

var richtungValues = []Richtung{RichtungBezug, RichtungEinspeisung}

// enumError is the rule text of every enum: the values are case-sensitive and closed, so a typo or a
// retired value such as "gemessen" fails loudly instead of being passed on.
func enumError[T ~string](v T, allowed []T) error {
	quoted := make([]string, len(allowed))
	for i, a := range allowed {
		quoted[i] = fmt.Sprintf("%q", string(a))
	}
	return ruleError("%q is not one of %s (case-sensitive)", string(v), strings.Join(quoted, ", "))
}

func checkEnum[T ~string](v T, allowed []T) error {
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return enumError(v, allowed)
}

func marshalEnum[T ~string](v T, allowed []T) ([]byte, error) {
	if err := checkEnum(v, allowed); err != nil {
		return nil, err
	}
	return json.Marshal(string(v))
}

func unmarshalEnum[T ~string](b []byte, allowed []T) (T, error) {
	s, err := unmarshalString(b)
	if err != nil {
		return "", err
	}
	v := T(s)
	if err := checkEnum(v, allowed); err != nil {
		return "", err
	}
	return v, nil
}

// Validate checks that s is one of the Status constants.
func (s Status) Validate() error { return checkEnum(s, statusValues) }

// MarshalJSON writes s as a JSON string after validating it.
func (s Status) MarshalJSON() ([]byte, error) { return marshalEnum(s, statusValues) }

// UnmarshalJSON reads a JSON string and validates it; null is rejected.
func (s *Status) UnmarshalJSON(b []byte) error {
	v, err := unmarshalEnum(b, statusValues)
	if err != nil {
		return err
	}
	*s = v
	return nil
}

// Validate checks that h is one of the Herkunft constants.
func (h Herkunft) Validate() error { return checkEnum(h, herkunftValues) }

// MarshalJSON writes h as a JSON string after validating it.
func (h Herkunft) MarshalJSON() ([]byte, error) { return marshalEnum(h, herkunftValues) }

// UnmarshalJSON reads a JSON string and validates it; null is rejected (a nullable herkunft is a
// *Herkunft, and the null is handled by the owning struct).
func (h *Herkunft) UnmarshalJSON(b []byte) error {
	v, err := unmarshalEnum(b, herkunftValues)
	if err != nil {
		return err
	}
	*h = v
	return nil
}

// Validate checks that p is one of the Profiltyp constants.
func (p Profiltyp) Validate() error { return checkEnum(p, profiltypValues) }

// MarshalJSON writes p as a JSON string after validating it.
func (p Profiltyp) MarshalJSON() ([]byte, error) { return marshalEnum(p, profiltypValues) }

// UnmarshalJSON reads a JSON string and validates it; null is rejected.
func (p *Profiltyp) UnmarshalJSON(b []byte) error {
	v, err := unmarshalEnum(b, profiltypValues)
	if err != nil {
		return err
	}
	*p = v
	return nil
}

// Validate checks that r is one of the Richtung constants.
func (r Richtung) Validate() error { return checkEnum(r, richtungValues) }

// MarshalJSON writes r as a JSON string after validating it.
func (r Richtung) MarshalJSON() ([]byte, error) { return marshalEnum(r, richtungValues) }

// UnmarshalJSON reads a JSON string and validates it; null is rejected.
func (r *Richtung) UnmarshalJSON(b []byte) error {
	v, err := unmarshalEnum(b, richtungValues)
	if err != nil {
		return err
	}
	*r = v
	return nil
}

// HerkunftMatchesStatus reports whether a herkunft is allowed for a status. A nil herkunft means
// "not set" (null in JSON); a non-nil pointer to the empty string is not "not set" but an unknown
// value and never matches (Python's herkunft_passt_zu_status treats "" the same way). The rules:
// fehlend if and only if herkunft is nil (a missing value has no origin); wahr requires fremd (the
// agent only forms provisional and substitute values, so a true value is always fremd); vorlaeufig and
// ersatzwert may have either. An unknown status or herkunft never matches.
func HerkunftMatchesStatus(status Status, herkunft *Herkunft) bool {
	if status.Validate() != nil {
		return false
	}
	if herkunft != nil && herkunft.Validate() != nil {
		return false
	}
	switch status {
	case StatusFehlend:
		return herkunft == nil
	case StatusWahr:
		return herkunft != nil && *herkunft == HerkunftFremd
	default:
		return herkunft != nil
	}
}

var maloPattern = regexp.MustCompile(`^[1-9][0-9]{10}$`)

// MaLoCheckDigit computes the check digit of a MaLo ID (BDEW algorithm) from its first ten digits:
// the digits at odd positions (1st, 3rd, ...) are summed once, those at even positions twice, and
// the check digit is (10 - sum % 10) % 10. It returns an error unless first10 is exactly ten ASCII
// digits (a leading zero is fine here; the ID rule in IsValidMaLoID is stricter).
func MaLoCheckDigit(first10 string) (int, error) {
	// Byte-wise on purpose: any non-ASCII character, including other scripts' digits, is rejected.
	if len(first10) != 10 {
		return 0, ruleError("a MaLo check digit needs exactly 10 digits, got %d bytes", len(first10))
	}
	total := 0
	for i := range 10 {
		c := first10[i]
		if c < '0' || c > '9' {
			return 0, ruleError("%q is not made of ASCII digits", first10)
		}
		d := int(c - '0')
		if i%2 == 1 {
			d *= 2
		}
		total += d
	}
	return (10 - total%10) % 10, nil
}

// IsValidMaLoID reports whether id is 11 ASCII digits, does not start with 0, and ends in the
// correct check digit.
func IsValidMaLoID(id string) bool {
	if !maloPattern.MatchString(id) {
		return false
	}
	cd, err := MaLoCheckDigit(id[:10])
	return err == nil && int(id[10]-'0') == cd
}

func checkMaLo(id string) error {
	if !maloPattern.MatchString(id) {
		return ruleError("%q is not 11 ASCII digits without a leading 0", id)
	}
	if !IsValidMaLoID(id) {
		return ruleError("%q has a wrong check digit", id)
	}
	return nil
}
