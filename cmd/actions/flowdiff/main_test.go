package main

import (
	"strings"
	"testing"
)

func TestFindSideBySideMarker_OnlyUsesSeparatorColumn(t *testing.T) {
	line := strings.Repeat("x", sideBySideWidth)
	b := []byte(line)
	// Marker-like token near the separator should be ignored.
	b[(sideBySideWidth/2)-4] = '<'
	b[(sideBySideWidth/2)-5] = ' '
	b[(sideBySideWidth/2)-3] = ' '
	b[(sideBySideWidth/2)-1] = ' '

	if _, _, ok := findSideBySideMarker(string(b)); ok {
		t.Fatalf("expected no marker when separator column has no marker")
	}
}

func TestFindSideBySideMarker_DetectsColumnMarker(t *testing.T) {
	line := strings.Repeat("x", sideBySideWidth)
	b := []byte(line)
	mid := (sideBySideWidth / 2) - 1
	b[mid-1] = ' '
	b[mid] = '|'
	b[mid+1] = ' '

	idx, marker, ok := findSideBySideMarker(string(b))
	if !ok {
		t.Fatalf("expected marker to be detected")
	}
	if idx != mid || marker != '|' {
		t.Fatalf("unexpected marker result: idx=%d marker=%q", idx, marker)
	}
}

func TestSuppressCommonSideBySideDiffLines(t *testing.T) {
	common := strings.Repeat("a", sideBySideWidth)
	changed := strings.Repeat("b", sideBySideWidth)

	b := []byte(changed)
	mid := (sideBySideWidth / 2) - 1
	b[mid-1] = ' '
	b[mid] = '|'
	b[mid+1] = ' '
	changed = string(b)

	got := suppressCommonSideBySideDiffLines(common + "\n" + changed + "\n")
	if strings.Contains(got, common) {
		t.Fatalf("expected common line to be removed")
	}
	if !strings.Contains(got, changed) {
		t.Fatalf("expected changed line to be retained")
	}
}
