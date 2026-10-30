package mdqmodels

// EnumValues exposes the complete, unexported value lists of the enums to the external tests, so a drift
// test can compare the proto with every value the package accepts, not with a hand-copied subset.
func EnumValues() map[string][]string {
	out := map[string][]string{}
	for _, v := range statusValues {
		out["Status"] = append(out["Status"], string(v))
	}
	for _, v := range herkunftValues {
		out["Herkunft"] = append(out["Herkunft"], string(v))
	}
	for _, v := range profiltypValues {
		out["Profiltyp"] = append(out["Profiltyp"], string(v))
	}
	for _, v := range richtungValues {
		out["Richtung"] = append(out["Richtung"], string(v))
	}
	return out
}
