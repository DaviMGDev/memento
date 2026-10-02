package loader_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/DaviMGDev/memento/loader"
)

type dbConfig struct {
	DSN string
}

func TestTreeRoundTripsNestedEntries(t *testing.T) {
	entries := []loader.Entry{
		{
			ID:        "app",
			Component: "app",
			Enabled:   true,
			Children: []loader.Entry{
				{ID: "db", Component: "database", Payload: dbConfig{DSN: "postgres://localhost"}, Enabled: true},
				{ID: "console", Component: "console", Enabled: false},
			},
		},
	}

	tree, err := loader.NewTree(entries...)
	if err != nil {
		t.Fatalf("NewTree() = %v", err)
	}

	got := tree.Roots()
	if !reflect.DeepEqual(got, entries) {
		t.Fatalf("Roots() = %#v, want the original nested entries", got)
	}

	// The returned tree is a copy.
	got[0].ID = "mutated"
	got[0].Children[0].ID = "mutated-child"
	if tree.Roots()[0].ID != "app" {
		t.Fatal("Roots leaked internal state through the returned copy")
	}
	if tree.Roots()[0].Children[0].ID != "db" {
		t.Fatal("Roots leaked nested state through the returned copy")
	}

	var walked []string
	tree.Walk(func(e loader.Entry) { walked = append(walked, e.ID) })
	if want := []string{"app", "db", "console"}; !reflect.DeepEqual(walked, want) {
		t.Fatalf("Walk order = %v, want %v", walked, want)
	}
}

func TestTreeRejectsInvalidIDs(t *testing.T) {
	cases := []struct {
		name    string
		entries []loader.Entry
		want    string
	}{
		{
			name: "duplicate siblings",
			entries: []loader.Entry{
				{ID: "a", Component: "x", Children: []loader.Entry{
					{ID: "dup", Component: "x"},
					{ID: "dup", Component: "x"},
				}},
			},
			want: "duplicate",
		},
		{
			name: "duplicate across nesting",
			entries: []loader.Entry{
				{ID: "app", Component: "x", Children: []loader.Entry{
					{ID: "app", Component: "x"},
				}},
			},
			want: "duplicate",
		},
		{
			name:    "empty id",
			entries: []loader.Entry{{ID: "", Component: "x"}},
			want:    "empty id",
		},
		{
			name:    "missing component",
			entries: []loader.Entry{{ID: "app"}},
			want:    "no component reference",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loader.NewTree(tc.entries...); err == nil {
				t.Fatal("NewTree() = nil, want a validation error")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("NewTree() error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
