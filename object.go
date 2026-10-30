package mdqmodels

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"
	"unicode/utf8"
)

// wireTimeLayout is the only accepted timestamp spelling: whole seconds and a literal Z.
const wireTimeLayout = "2006-01-02T15:04:05Z"

// timestampPattern guards the calendar parse. Why exactly Z: the wire format is UTC and a single
// spelling per instant keeps Go and Python bytes identical ("+00:00" and "z" would be the same
// instant written differently). Go's $ matches only at the end of the text, and \d is ASCII-only in
// Go, so neither a trailing newline nor other scripts' digits get through.
var timestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

func parseTimestamp(s string) (time.Time, error) {
	if !timestampPattern.MatchString(s) {
		return time.Time{}, ruleError("%q is not of the form YYYY-MM-DDTHH:MM:SSZ (UTC, offset exactly Z, whole seconds)", s)
	}
	t, err := time.Parse(wireTimeLayout, s)
	if err != nil {
		return time.Time{}, ruleError("%q is not a real calendar time", s)
	}
	return t, nil
}

// checkTimestamp validates a timestamp built in code. Any time zone is accepted (the instant is what
// counts and it is written in UTC); the zero time is rejected because it almost always means "not
// set". A naive timestamp cannot exist in Go, so there is no rule for it.
func checkTimestamp(t time.Time, quarterHour bool) error {
	if t.IsZero() {
		return ruleError("must not be the zero time")
	}
	u := t.UTC()
	// Below 1900 there is no real data; 9999 keeps the year four digits.
	if u.Year() < 1900 || u.Year() > 9999 {
		return ruleError("year %d is outside 1900-9999", u.Year())
	}
	if u.Nanosecond() != 0 {
		return ruleError("must be a whole second")
	}
	// Checked on the UTC instant, not on the local clock: some zones are offset by 45 minutes.
	if quarterHour && (u.Minute()%15 != 0 || u.Second() != 0) {
		return ruleError("must be the start of a quarter hour (minute divisible by 15, second 0)")
	}
	return nil
}

func formatTimestamp(t time.Time) string { return t.UTC().Format(wireTimeLayout) }

// decodeObject reads b as one JSON object whose keys are exactly allowed (any order) and returns the
// raw value of each key. It rejects, with a *ValidationError naming the key:
//   - unknown keys: a typo must fail loudly, and a new field means all consumers update together;
//   - duplicate keys at any nesting level: encoding/json would silently keep the last value, so two
//     parsers (or a reviewer and a parser) could read different data from the same bytes;
//   - missing keys, also when the value would have been null: "absent" and "null" are different
//     statements, and required-nullable fields must always be written;
//   - anything that is not a single object (null, arrays, scalars, trailing data).
func decodeObject(b []byte, allowed []string) (map[string]json.RawMessage, error) {
	// encoding/json silently repairs invalid UTF-8 to U+FFFD; JSON text is UTF-8 (RFC 8259), so
	// refuse it. A byte order mark or UTF-16 text fails below as a syntax error.
	if !utf8.Valid(b) {
		return nil, ruleError("input is not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber() // see checkNoDuplicateKeys
	tok, err := dec.Token()
	if err != nil {
		return nil, ruleError("must be a JSON object: not valid JSON (%v)", err)
	}
	if tok != json.Delim('{') {
		return nil, ruleError("must be a JSON object, not %v", tok)
	}
	fields := make(map[string]json.RawMessage, len(allowed))
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, ruleError("not valid JSON (%v)", err)
		}
		key, _ := keyTok.(string) // object keys are always strings
		if !contains(allowed, key) {
			return nil, &ValidationError{Field: key, Rule: "unknown key"}
		}
		if _, seen := fields[key]; seen {
			return nil, &ValidationError{Field: key, Rule: "duplicate key"}
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, ruleError("not valid JSON (%v)", err)
		}
		if err := checkNoDuplicateKeys(raw); err != nil {
			return nil, &ValidationError{Field: key, Rule: "duplicate key inside the value"}
		}
		fields[key] = raw
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return nil, ruleError("not valid JSON (%v)", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) { // also true for syntax errors after the object
		return nil, ruleError("must be a single JSON object, found trailing data")
	}
	for _, key := range allowed {
		if _, ok := fields[key]; !ok {
			return nil, &ValidationError{Field: key, Rule: "is required"}
		}
	}
	return fields, nil
}

var errDuplicateKey = errors.New("duplicate key")

// checkNoDuplicateKeys walks one JSON value and fails if any object in it repeats a key.
func checkNoDuplicateKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	// UseNumber: without it Token() converts numbers to float64 and fails on "1e1000", which would be
	// misreported as a duplicate key. The number is rejected later, for the right reason (not a string).
	dec.UseNumber()
	return walkDuplicates(dec)
}

func walkDuplicates(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for dec.More() {
		if delim == '{' {
			k, err := dec.Token()
			if err != nil {
				return err
			}
			key, _ := k.(string)
			if seen[key] {
				return errDuplicateKey
			}
			seen[key] = true
		}
		if err := walkDuplicates(dec); err != nil {
			return err
		}
	}
	_, err = dec.Token() // the closing delimiter
	return err
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// stringField decodes the value of a required key that must be a non-null JSON string.
func stringField(fields map[string]json.RawMessage, key string) (string, error) {
	s, err := unmarshalString(fields[key])
	return s, inField(key, err)
}

// nullableStringField decodes a required-nullable key: a JSON string, or nil for null.
func nullableStringField(fields map[string]json.RawMessage, key string) (*string, error) {
	if isNull(fields[key]) {
		return nil, nil //nolint:nilnil // null is a legal value here; the caller's rules decide about it
	}
	s, err := stringField(fields, key)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// timestampField decodes the strict timestamp spelling.
func timestampField(fields map[string]json.RawMessage, key string) (time.Time, error) {
	s, err := stringField(fields, key)
	if err != nil {
		return time.Time{}, err
	}
	t, err := parseTimestamp(s)
	return t, inField(key, err)
}
