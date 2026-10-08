package mcp

import (
	"strings"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		err  bool
	}{
		{"", 0, false},
		{"15m", 15 * time.Minute, false},
		{"1h", time.Hour, false},
		{"24h", 24 * time.Hour, false},
		{"30", 30 * time.Minute, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"  ", 0, false},
		{"bogus", 0, true},
		{"xd", 0, true},
		{"-1h", 0, true},
		{"0", 0, true},
	}
	for _, tc := range cases {
		got, err := parseDuration(tc.in)
		if tc.err {
			if err == nil {
				t.Errorf("parseDuration(%q) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDuration(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseDuration(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a,b,c", []string{"a", "b", "c"}},
		{"a, b, ,c", []string{"a", "b", "c"}},
		{"  a  ,  b  ", []string{"a", "b"}},
		{",", nil},
	}
	for _, tc := range cases {
		got := splitCSV(tc.in)
		if !stringSliceEq(got, tc.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func stringSliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestResolveTimeWindowDefaults(t *testing.T) {
	before := time.Now()
	start, end, err := resolveTimeWindow("", "", "")
	if err != nil {
		t.Fatalf("resolveTimeWindow default: %v", err)
	}
	if end.Before(before) {
		t.Errorf("end %v before now %v", end, before)
	}
	if end.Sub(start) != time.Hour {
		t.Errorf("default window = %v, want 1h", end.Sub(start))
	}
}

func TestResolveTimeWindowSince(t *testing.T) {
	start, end, err := resolveTimeWindow("", "", "30m")
	if err != nil {
		t.Fatalf("resolveTimeWindow since: %v", err)
	}
	if end.Sub(start) != 30*time.Minute {
		t.Errorf("since=30m window = %v, want 30m", end.Sub(start))
	}
}

func TestResolveTimeWindowEndTimeDefaultsStart(t *testing.T) {
	endStr := "2026-01-02T03:04:05Z"
	start, end, err := resolveTimeWindow("", endStr, "")
	if err != nil {
		t.Fatalf("resolveTimeWindow end_time: %v", err)
	}
	wantEnd, _ := time.Parse(time.RFC3339, endStr)
	if !end.Equal(wantEnd) {
		t.Errorf("end = %v, want %v", end, wantEnd)
	}
	if end.Sub(start) != time.Hour {
		t.Errorf("start-end = %v, want 1h (default for missing start_time)", end.Sub(start))
	}
}

func TestResolveTimeWindowInvalidEndTime(t *testing.T) {
	if _, _, err := resolveTimeWindow("", "not-rfc3339", ""); err == nil {
		t.Fatal("expected error for invalid end_time")
	} else if !strings.Contains(err.Error(), "end_time") {
		t.Errorf("error should mention end_time, got %q", err.Error())
	}
}

func TestWindowFromArgsDefaults(t *testing.T) {
	rng, err := windowFromArgs(map[string]any{}, "1h")
	if err != nil {
		t.Fatalf("windowFromArgs empty: %v", err)
	}
	if rng.End.Sub(rng.Start) != time.Hour {
		t.Errorf("default window = %v, want 1h", rng.End.Sub(rng.Start))
	}
}

func TestWindowFromArgsSince(t *testing.T) {
	rng, err := windowFromArgs(map[string]any{"since": "45m"}, "1h")
	if err != nil {
		t.Fatalf("windowFromArgs since: %v", err)
	}
	if rng.End.Sub(rng.Start) != 45*time.Minute {
		t.Errorf("since=45m window = %v, want 45m", rng.End.Sub(rng.Start))
	}
}

func TestWindowFromArgsStartEndOverride(t *testing.T) {
	endStr := "2026-01-02T03:04:05Z"
	startStr := "2026-01-02T00:00:00Z"
	rng, err := windowFromArgs(map[string]any{
		"start_time": startStr,
		"end_time":   endStr,
		"since":      "ignored",
	}, "1h")
	if err != nil {
		t.Fatalf("windowFromArgs start/end: %v", err)
	}
	wantStart, _ := time.Parse(time.RFC3339, startStr)
	wantEnd, _ := time.Parse(time.RFC3339, endStr)
	if !rng.Start.Equal(wantStart) || !rng.End.Equal(wantEnd) {
		t.Errorf("got [%v, %v], want [%v, %v]", rng.Start, rng.End, wantStart, wantEnd)
	}
}

func TestWindowFromArgsUsesToolDefault(t *testing.T) {
	rng, err := windowFromArgs(map[string]any{}, "24h")
	if err != nil {
		t.Fatalf("windowFromArgs: %v", err)
	}
	if rng.End.Sub(rng.Start) != 24*time.Hour {
		t.Errorf("default window = %v, want 24h", rng.End.Sub(rng.Start))
	}
}

func TestResolveTimeWindowRejectsInvertedRange(t *testing.T) {
	_, _, err := resolveTimeWindow("2026-01-02T00:00:00Z", "2026-01-01T00:00:00Z", "")
	if err == nil {
		t.Fatal("expected error when start_time is after end_time")
	}
}
