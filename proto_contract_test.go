package mdqmodels_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	mdq "github.com/hochfrequenz/mdq-models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The proto is the gRPC contract; these tests keep it in step with the hand-written Go types and
// with the pinned files. unittests/test_proto_contract.py is the Python counterpart.

var (
	protoBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	protoLineComment  = regexp.MustCompile(`//[^\n]*`)
	protoBlockOpen    = regexp.MustCompile(`^(message|enum) (\w+) \{$`)
	protoBlockEmpty   = regexp.MustCompile(`^(message|enum) (\w+) \{\s*\}$`)
	protoTopLevel     = regexp.MustCompile(`^(syntax|package|import|option)\b[^;]*;$`)
	protoInnerSkip    = regexp.MustCompile(`^(option|reserved)\b[^;]*;$`)
	protoFieldDecl    = regexp.MustCompile(`^((?:optional\s+|repeated\s+)?(?:map\s*<[^>]+>|[\w.]+))\s+(\w+)\s*=\s*(\d+)\s*(?:\[[^\]]*\])?\s*;$`)
	protoEnumValue    = regexp.MustCompile(`^(\w+)\s*=\s*(-?\d+)\s*(?:\[[^\]]*\])?\s*;$`)
	protoMapSpaces    = regexp.MustCompile(`\s*([<,>])\s*`)
)

// parseProto reads proto3 text and returns message -> field -> "<label type> = <number>" and
// enum -> constant -> number. It fails on any line it cannot classify (nested messages, oneof,
// multi-line statements, ...): a drift test must never skip a declaration silently, so new syntax
// has to be taught to the parser first. Field options are accepted and ignored.
func parseProto(text string) (map[string]map[string]string, map[string]map[string]int, error) {
	text = protoLineComment.ReplaceAllString(protoBlockComment.ReplaceAllString(text, ""), "")
	messages := map[string]map[string]string{}
	enums := map[string]map[string]int{}
	kind, name := "", ""
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		fail := func(format string, args ...any) error {
			return fmt.Errorf("proto line %d %q: "+format, append([]any{i + 1, line}, args...)...)
		}
		switch {
		case line == "":
		case kind == "" && protoTopLevel.MatchString(line):
		case kind == "" && protoBlockEmpty.MatchString(line), kind == "" && protoBlockOpen.MatchString(line):
			m := protoBlockOpen.FindStringSubmatch(line)
			empty := false
			if m == nil {
				m = protoBlockEmpty.FindStringSubmatch(line)
				empty = true
			}
			k, n := m[1], m[2]
			if _, dup := messages[n]; dup {
				return nil, nil, fail("%s declared twice", n)
			}
			if _, dup := enums[n]; dup {
				return nil, nil, fail("%s declared twice", n)
			}
			if k == "message" {
				messages[n] = map[string]string{}
			} else {
				enums[n] = map[string]int{}
			}
			if !empty {
				kind, name = k, n
			}
		case kind != "" && line == "}":
			kind, name = "", ""
		case kind != "" && protoInnerSkip.MatchString(line):
		case kind == "message" && protoFieldDecl.MatchString(line):
			f := protoFieldDecl.FindStringSubmatch(line)
			typ := strings.Join(strings.Fields(f[1]), " ")
			typ = protoMapSpaces.ReplaceAllString(typ, "$1") // map< k , v > -> map<k,v>
			if _, dup := messages[name][f[2]]; dup {
				return nil, nil, fail("field %s.%s declared twice", name, f[2])
			}
			messages[name][f[2]] = typ + " = " + f[3]
		case kind == "enum" && protoEnumValue.MatchString(line):
			v := protoEnumValue.FindStringSubmatch(line)
			n, err := strconv.Atoi(v[2])
			if err != nil {
				return nil, nil, fail("%v", err)
			}
			if _, dup := enums[name][v[1]]; dup {
				return nil, nil, fail("constant %s.%s declared twice", name, v[1])
			}
			enums[name][v[1]] = n
		default:
			return nil, nil, fail("not understood by the drift-test parser (inside %q %q)", kind, name)
		}
	}
	if kind != "" {
		return nil, nil, fmt.Errorf("proto: %s %s is not closed", kind, name)
	}
	return messages, enums, nil
}

func protoFiles(t *testing.T) (map[string]map[string]string, map[string]map[string]int) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("proto", "mdq", "v1", "*.proto"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "no .proto files")
	messages := map[string]map[string]string{}
	enums := map[string]map[string]int{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		m, e, err := parseProto(string(raw))
		require.NoError(t, err, f)
		for k, v := range m {
			require.NotContains(t, messages, k, "message %s declared in two files", k)
			messages[k] = v
		}
		for k, v := range e {
			require.NotContains(t, enums, k, "enum %s declared in two files", k)
			enums[k] = v
		}
	}
	return messages, enums
}

func protoFields(t *testing.T) map[string]map[string]string {
	t.Helper()
	m, _ := protoFiles(t)
	return m
}

func protoEnums(t *testing.T) map[string]map[string]int {
	t.Helper()
	_, e := protoFiles(t)
	return e
}

const protoSnippet = `
syntax = "proto3";
package x.v1;
import "google/protobuf/timestamp.proto";
option java_package = "x";

/* block comment
message Hidden { string hidden = 1; }
*/
message Empty {}
message All {
  // string commented_out = 9;
  string plain = 1;
  optional string opt = 2;
  repeated google.protobuf.Timestamp rep = 3;
  map<string, int64> kv = 4;
  string extra = 5 [deprecated = true];
  reserved 6, 7;
  option deprecated = true;
}
enum Neg {
  NEG_UNSPECIFIED = 0;
  NEG_NEU = -1;
  NEG_OLD = 2 [deprecated = true];
  reserved 3;
}
`

func TestParseProtoSnippet(t *testing.T) {
	messages, enums, err := parseProto(protoSnippet)
	require.NoError(t, err)
	assert.Equal(t, map[string]map[string]string{
		"Empty": {},
		"All": {
			"plain": "string = 1", "opt": "optional string = 2", "rep": "repeated google.protobuf.Timestamp = 3",
			"kv": "map<string,int64> = 4", "extra": "string = 5",
		},
	}, messages)
	assert.Equal(t, map[string]map[string]int{"Neg": {"NEG_UNSPECIFIED": 0, "NEG_NEU": -1, "NEG_OLD": 2}}, enums)
}

func TestParseProtoFailsLoudlyOnUnknownSyntax(t *testing.T) {
	for _, bad := range []string{
		"message M {\n  oneof o {\n    string a = 1;\n  }\n}\n",
		"message M {\n  message Inner {}\n}\n",
		"message M {\n  string a =\n 1;\n}\n",
		"enum E {\n  E_A = 0; E_B = 1;\n}\n",
		"message M {\n  string a = 1;\n",
		"message M {\n  string a = 1;\n  string b = 1x;\n}\n",
		"service S {\n  rpc A(M) returns (M);\n}\n",
	} {
		_, _, err := parseProto(bad)
		assert.Error(t, err, bad)
	}
}

func TestProtoFieldNumbersAndTypesArePinned(t *testing.T) {
	var pinned map[string]map[string]string
	require.NoError(t, json.Unmarshal(fixtureDuplicateFree(t, "proto_fields.json"), &pinned))
	current := protoFields(t)
	assert.Equal(t, pinned, current, "proto_fields.json pins every field with its label, type and number; update both together, and never change an existing number or type")
	for message, fields := range current {
		seen := map[string]string{}
		for field, declaration := range fields {
			number := declaration[strings.LastIndex(declaration, " = ")+3:]
			assert.NotContains(t, seen, number, "%s reuses field number %s (%s, %s)", message, number, field, seen[number])
			seen[number] = field
		}
	}
}

// jsonKeys returns the keys MarshalJSON writes for v. The proto field names are the JSON keys, so
// deriving them from the real output (not from Go field names) also covers multi-word names.
func jsonKeys(t *testing.T, v any) []string {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &m))
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestProtoMessageFieldsEqualGoFields(t *testing.T) {
	zaehlerstand := mdq.Zaehlerstand{
		Malo:      "51238696781",
		Timestamp: time.Date(2026, 1, 15, 10, 15, 1, 0, time.UTC),
		Value:     mdq.Decimal("1.5"),
	}
	models := map[string]any{"Messwert": validMesswert(), "Zaehlerstand": zaehlerstand}
	proto := protoFields(t)
	require.Len(t, proto, len(models), "every proto message needs a Go model")
	for name, sample := range models {
		protoNames := make([]string, 0, len(proto[name]))
		for field := range proto[name] {
			protoNames = append(protoNames, field)
		}
		sort.Strings(protoNames)
		assert.Equal(t, jsonKeys(t, sample), protoNames, name)

		// The serializer is hand-written, so also compare the struct itself: a new exported Go field
		// that MarshalJSON ignores must fail here, and so must one missing from the mapping table.
		typ := reflect.TypeOf(sample)
		goKeys := make([]string, 0, typ.NumField())
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			key, ok := goFieldJSONKeys[name][field.Name]
			require.True(t, ok, "%s.%s is not in goFieldJSONKeys: map it to its JSON key and add it to the serializer and the proto", name, field.Name)
			goKeys = append(goKeys, key)
		}
		sort.Strings(goKeys)
		assert.Equal(t, goKeys, protoNames, "%s: exported Go fields vs proto fields", name)
	}
}

// goFieldJSONKeys maps every exported field of the Go models to its JSON key (= proto field name).
var goFieldJSONKeys = map[string]map[string]string{
	"Messwert": {
		"Malo": "malo", "Timestamp": "timestamp", "Resolution": "resolution", "Value": "value",
		"Status": "status", "Herkunft": "herkunft", "Profiltyp": "profiltyp", "Richtung": "richtung",
	},
	"Zaehlerstand": {"Malo": "malo", "Timestamp": "timestamp", "Value": "value"},
}

func TestProtoEnumsMatchPinnedFileAndGoEnums(t *testing.T) {
	var pinned map[string]struct {
		Unspecified string `json:"unspecified"`
		Values      []struct {
			Name   string `json:"name"`
			Number int    `json:"number"`
			JSON   string `json:"json"`
		} `json:"values"`
	}
	require.NoError(t, json.Unmarshal(fixtureDuplicateFree(t, "proto_enums.json"), &pinned))
	proto := protoEnums(t)
	require.Len(t, proto, len(pinned))
	goEnums := mdq.EnumValues() // every value the package accepts, not a hand-copied subset
	require.Len(t, goEnums, len(pinned))
	validators := map[string]func(string) error{
		"Status":    func(s string) error { return mdq.Status(s).Validate() },
		"Herkunft":  func(s string) error { return mdq.Herkunft(s).Validate() },
		"Profiltyp": func(s string) error { return mdq.Profiltyp(s).Validate() },
		"Richtung":  func(s string) error { return mdq.Richtung(s).Validate() },
	}
	for name, p := range pinned {
		// proto <-> pinned file, both directions: the maps must be equal.
		expected := map[string]int{p.Unspecified: 0}
		var jsonValues []string
		for _, v := range p.Values {
			expected[v.Name] = v.Number
			jsonValues = append(jsonValues, v.JSON)
			assert.NoError(t, validators[name](v.JSON), "%s %s", name, v.JSON)
			assert.Equal(t, strings.ToLower(strings.TrimPrefix(v.Name, strings.ToUpper(name)+"_")), v.JSON)
		}
		assert.Equal(t, expected, proto[name], name)
		// pinned file <-> Go enum, both directions.
		assert.Equal(t, goEnums[name], jsonValues, name)
		// Go -> proto: every value the package accepts has a proto constant, derived from its name.
		for _, v := range goEnums[name] {
			assert.Contains(t, proto[name], strings.ToUpper(name)+"_"+strings.ToUpper(v), "Go value %s.%s has no proto constant", name, v)
		}
		// *_UNSPECIFIED is never a JSON value.
		assert.Equal(t, strings.ToUpper(name)+"_UNSPECIFIED", p.Unspecified)
		assert.NotContains(t, jsonValues, "unspecified")
		assert.Error(t, validators[name]("unspecified"), name)
		assert.Error(t, validators[name](""), name)
	}
}
