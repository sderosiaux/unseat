// Package golden holds real-API response shapes, captured once from a live
// tenant and anonymised, so connector tests can replay the actual wire format
// instead of marshalling our own structs.
//
// See README.md for what a golden file is, how to refresh one, and the
// anonymisation rule.
package golden

import (
	"embed"
	"testing"
)

//go:embed testdata
var testdataFS embed.FS

// Load returns the raw bytes of a golden file, e.g.
// golden.Load(t, "linear-users.json").
func Load(t testing.TB, name string) []byte {
	t.Helper()
	data, err := testdataFS.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("golden: load %s: %v", name, err)
	}
	return data
}
