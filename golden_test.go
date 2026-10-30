package mdqmodels_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite testdata/serialized.golden from the Go output")

// goldenFile pins the exact bytes written for every valid fixture: one line per fixture,
// "<kind>/<file>\t<serialized JSON>", sorted. The Python tests compare against the same file, so Go
// and Python output is byte-for-byte identical.
var goldenFile = filepath.Join(fixtureDir, "serialized.golden")

func serializedFixtures(t *testing.T) string {
	t.Helper()
	var lines []string
	for _, name := range fixtureFiles(t, "valid") {
		kind := strings.SplitN(name, "/", 2)[0]
		v, err := kinds[kind].parse(fixtureRead(t, name))
		require.NoError(t, err, name)
		out, err := kinds[kind].dump(v)
		require.NoError(t, err, name)
		lines = append(lines, kind+"/"+filepath.Base(name)+"\t"+string(out))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func TestSerializedBytesMatchGolden(t *testing.T) {
	got := serializedFixtures(t)
	if *update {
		require.NoError(t, os.WriteFile(goldenFile, []byte(got), 0o600))
	}
	assert.Equal(t, string(fixtureRead(t, "serialized.golden")), got,
		"run `go test -run Golden -update` after an intended change")
}

// fixtureNamePattern keeps fixture names portable: they end up in the golden file and in test ids in
// both languages, so no spaces, upper case, tabs or non-ASCII characters.
var fixtureNamePattern = regexp.MustCompile(`^[a-z0-9_]+\.json$`)

func TestFixtureFileNamesArePortable(t *testing.T) {
	for _, validity := range []string{"valid", "invalid"} {
		for _, name := range fixtureFiles(t, validity) {
			assert.Regexp(t, fixtureNamePattern, filepath.Base(name), name)
		}
	}
}

func TestGoldenFileFormat(t *testing.T) {
	raw := string(fixtureRead(t, "serialized.golden"))
	require.True(t, strings.HasSuffix(raw, "\n"))
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	assert.True(t, sort.StringsAreSorted(lines), "lines are sorted by bytes")
	assert.NotContains(t, raw, "\r")
	for _, l := range lines {
		name, doc, ok := strings.Cut(l, "\t")
		require.True(t, ok, l)
		assert.Regexp(t, `^(messwert|zaehlerstand)/[a-z0-9_]+\.json$`, name)
		var compact bytes.Buffer
		require.NoError(t, json.Compact(&compact, []byte(doc)))
		assert.Equal(t, compact.String(), doc, "compact JSON without spaces")
	}
}
