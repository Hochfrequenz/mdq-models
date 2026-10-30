// Package mdqmodels holds the hand-written data models of the canonical meter-reading schema:
// Messwert (one value per quarter hour) and Zaehlerstand (a meter reading).
//
// The contract is the JSON form of the types in this package, pinned by the shared fixtures in
// testdata/, which the Go and the Python models both parse and round-trip. The gRPC contract lives
// in proto/mdq/v1.
//
// Reading is strict on purpose: unknown and duplicate keys are rejected, so a typo fails loudly and
// two parsers can never read different data from the same bytes. Every rejection is a
// *ValidationError naming the field and the rule.
package mdqmodels
