// Package textdiff implements a line based Myers diff producing unified diffs.
package textdiff

import (
	"fmt"
	"strings"
)

// Op is a diff operation.
type Op struct {
	Kind byte // ' ', '-', '+'
	Text string
}

// Lines computes the line diff between a and b. Common prefixes and suffixes
// are trimmed first; the remaining middle is diffed with an LCS table. Very
// large middles fall back to a replace-all hunk to bound memory.
func Lines(a, b string) []Op {
	x := splitLines(a)
	y := splitLines(b)
	var head, tail []Op
	for len(x) > 0 && len(y) > 0 && x[0] == y[0] {
		head = append(head, Op{' ', x[0]})
		x, y = x[1:], y[1:]
	}
	for len(x) > 0 && len(y) > 0 && x[len(x)-1] == y[len(y)-1] {
		tail = append([]Op{{' ', x[len(x)-1]}}, tail...)
		x, y = x[:len(x)-1], y[:len(y)-1]
	}
	var mid []Op
	n, m := len(x), len(y)
	if n*m > 16_000_000 {
		for _, l := range x {
			mid = append(mid, Op{'-', l})
		}
		for _, l := range y {
			mid = append(mid, Op{'+', l})
		}
	} else {
		// lcs[i][j] = LCS length of x[i:], y[j:]
		lcs := make([][]int32, n+1)
		for i := range lcs {
			lcs[i] = make([]int32, m+1)
		}
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if x[i] == y[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else if lcs[i+1][j] >= lcs[i][j+1] {
					lcs[i][j] = lcs[i+1][j]
				} else {
					lcs[i][j] = lcs[i][j+1]
				}
			}
		}
		i, j := 0, 0
		for i < n && j < m {
			switch {
			case x[i] == y[j]:
				mid = append(mid, Op{' ', x[i]})
				i++
				j++
			case lcs[i+1][j] >= lcs[i][j+1]:
				mid = append(mid, Op{'-', x[i]})
				i++
			default:
				mid = append(mid, Op{'+', y[j]})
				j++
			}
		}
		for ; i < n; i++ {
			mid = append(mid, Op{'-', x[i]})
		}
		for ; j < m; j++ {
			mid = append(mid, Op{'+', y[j]})
		}
	}
	out := append(head, mid...)
	return append(out, tail...)
}

// Unified renders a unified diff with the given number of context lines.
func Unified(fromName, toName, a, b string, context int) string {
	ops := Lines(a, b)
	changed := false
	for _, op := range ops {
		if op.Kind != ' ' {
			changed = true
			break
		}
	}
	if !changed {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", fromName, toName)
	// Group into hunks.
	type idx struct {
		op   Op
		a, b int
	}
	var all []idx
	ai, bi := 1, 1
	for _, op := range ops {
		all = append(all, idx{op, ai, bi})
		switch op.Kind {
		case ' ':
			ai++
			bi++
		case '-':
			ai++
		case '+':
			bi++
		}
	}
	for s := 0; s < len(all); {
		// find next change
		for s < len(all) && all[s].op.Kind == ' ' {
			s++
		}
		if s >= len(all) {
			break
		}
		start := s - context
		if start < 0 {
			start = 0
		}
		end := s
		for end < len(all) {
			if all[end].op.Kind != ' ' {
				end++
				continue
			}
			// count run of context
			run := 0
			for end+run < len(all) && all[end+run].op.Kind == ' ' {
				run++
			}
			if end+run >= len(all) || run > 2*context {
				end += min(run, context)
				break
			}
			end += run
		}
		h := all[start:end]
		var ac, bc int
		for _, e := range h {
			if e.op.Kind != '+' {
				ac++
			}
			if e.op.Kind != '-' {
				bc++
			}
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", h[0].a, ac, h[0].b, bc)
		for _, e := range h {
			sb.WriteByte(e.op.Kind)
			sb.WriteString(e.op.Text)
			sb.WriteByte('\n')
		}
		s = end
	}
	return sb.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}
