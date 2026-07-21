package report

import (
	"reflect"
	"testing"
)

func TestBuildZipLayoutListsOnlyPrefixedDeliverablesAndIsSorted(t *testing.T) {
	manifest, err := BuildZipManifest("task-1", []string{"src/z.js", "src/pages/a.js"}, "src")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src/pages/a.js", "src/z.js"}
	if !reflect.DeepEqual(manifest.Files, want) {
		t.Fatalf("files = %#v, want %#v", manifest.Files, want)
	}
}

func TestBuildZipManifestRejectsUnsafeOutsideAndDuplicateEntries(t *testing.T) {
	for _, entries := range [][]string{
		{"src/../../escape.js"},
		{`src/..\..\escape.js`},
		{"reports/private.json"},
		{"src/app.js", "src/app.js"},
	} {
		if _, err := BuildZipManifest("task-1", entries, "src"); err == nil {
			t.Fatalf("expected unsafe manifest entries %#v to be rejected", entries)
		}
	}
}
