package textdiff

import (
	"strings"
	"testing"
)

func TestUnified(t *testing.T) {
	a := "a\nb\nc\nd\ne\n"
	b := "a\nb\nX\nd\ne\nf\n"
	out := Unified("live", "local", a, b, 1)
	for _, want := range []string{"-c", "+X", "+f", "--- live"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
	if Unified("a", "b", a, a, 3) != "" {
		t.Fatal("identical inputs should produce empty diff")
	}
}

func TestLinesRoundTrip(t *testing.T) {
	cases := [][2]string{{"", "x\n"}, {"x\n", ""}, {"a\nb\n", "b\na\n"}, {"1\n2\n3\n4\n", "0\n2\n4\n5\n"}}
	for _, c := range cases {
		var from, to []string
		for _, op := range Lines(c[0], c[1]) {
			if op.Kind != '+' {
				from = append(from, op.Text)
			}
			if op.Kind != '-' {
				to = append(to, op.Text)
			}
		}
		if got := strings.Join(from, "\n"); got != strings.TrimSuffix(c[0], "\n") {
			t.Fatalf("from mismatch %q vs %q", got, c[0])
		}
		if got := strings.Join(to, "\n"); got != strings.TrimSuffix(c[1], "\n") {
			t.Fatalf("to mismatch %q vs %q", got, c[1])
		}
	}
}
