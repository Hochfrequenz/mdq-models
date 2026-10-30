package mdqmodels

import "fmt"

// ValidationError says which field broke which rule. Every error returned by this package when it
// rejects data is a *ValidationError, so callers can use errors.As to find the field and the rule
// instead of matching text. That holds for Validate, MarshalJSON, ParseMesswert and
// ParseZaehlerstand; it does NOT hold for json.Unmarshal, which reports syntax errors (including
// trailing data) as *json.SyntaxError before this package sees the input. Use the Parse functions
// to read untrusted bytes.
type ValidationError struct {
	// Field is the JSON key the rule belongs to ("malo", "value", ...). It is empty when the rule
	// concerns the document as a whole (for example "must be a JSON object").
	Field string
	// Rule is a short human-readable statement of the rule that was broken.
	Rule string
}

// Error implements error.
func (e *ValidationError) Error() string {
	if e.Field == "" {
		return "mdqmodels: " + e.Rule
	}
	return fmt.Sprintf("mdqmodels: %s: %s", e.Field, e.Rule)
}

// ruleError builds a ValidationError without a field; callers that know the field attach it with
// inField.
func ruleError(format string, args ...any) *ValidationError {
	return &ValidationError{Rule: fmt.Sprintf(format, args...)}
}

// inField attaches the field name to a rule error from a type-level Validate (Decimal, Status, ...),
// which does not know which field it sits in. A nil error stays nil.
func inField(field string, err error) error {
	if err == nil {
		return nil
	}
	if ve, ok := err.(*ValidationError); ok { //nolint:errorlint // we only ever return *ValidationError ourselves
		return &ValidationError{Field: field, Rule: ve.Rule}
	}
	return &ValidationError{Field: field, Rule: err.Error()}
}
