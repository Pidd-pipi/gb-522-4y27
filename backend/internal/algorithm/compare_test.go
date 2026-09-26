package algorithm

import (
	"testing"
	"time"
)

func TestCheckTraceCompatibilityTable(t *testing.T) {
	captured := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	baseline := TraceConditions{WavelengthNM: 1550, PulseWidthNS: 100, SampleIntervalNS: 0.5, CapturedAt: captured}
	later := captured.Add(time.Hour)
	tests := []struct {
		name       string
		current    TraceConditions
		compatible bool
		summary    string
	}{
		{"matching conditions and newer capture", TraceConditions{WavelengthNM: 1550, PulseWidthNS: 100, SampleIntervalNS: 0.5, CapturedAt: later}, true, ""},
		{"wavelength mismatch", TraceConditions{WavelengthNM: 1310, PulseWidthNS: 100, SampleIntervalNS: 0.5, CapturedAt: later}, false, "wavelength differs (baseline 1550 nm, current 1310 nm)"},
		{"pulse width mismatch", TraceConditions{WavelengthNM: 1550, PulseWidthNS: 200, SampleIntervalNS: 0.5, CapturedAt: later}, false, "pulse width differs (baseline 100 ns, current 200 ns)"},
		{"sample interval mismatch", TraceConditions{WavelengthNM: 1550, PulseWidthNS: 100, SampleIntervalNS: 1, CapturedAt: later}, false, "sample interval differs (baseline 0.5 ns, current 1 ns)"},
		{"current captured before baseline", TraceConditions{WavelengthNM: 1550, PulseWidthNS: 100, SampleIntervalNS: 0.5, CapturedAt: captured.Add(-time.Hour)}, false, "current trace captured at 2026-09-01T07:00:00Z is not later than the baseline capture at 2026-09-01T08:00:00Z"},
		{"same capture instant rejected", TraceConditions{WavelengthNM: 1550, PulseWidthNS: 100, SampleIntervalNS: 0.5, CapturedAt: captured}, false, "current trace captured at 2026-09-01T08:00:00Z is not later than the baseline capture at 2026-09-01T08:00:00Z"},
		{"multiple failures itemized", TraceConditions{WavelengthNM: 1310, PulseWidthNS: 200, SampleIntervalNS: 0.5, CapturedAt: captured}, false, "wavelength differs (baseline 1550 nm, current 1310 nm); pulse width differs (baseline 100 ns, current 200 ns); current trace captured at 2026-09-01T08:00:00Z is not later than the baseline capture at 2026-09-01T08:00:00Z"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := CheckTraceCompatibility(baseline, test.current)
			if len(result.Checks) != 4 {
				t.Fatalf("got %d checks, want 4", len(result.Checks))
			}
			if result.Compatible != test.compatible {
				t.Fatalf("compatible = %v, want %v (checks: %#v)", result.Compatible, test.compatible, result.Checks)
			}
			if summary := result.FailureSummary(); summary != test.summary {
				t.Fatalf("summary = %q, want %q", summary, test.summary)
			}
		})
	}
}

func TestCheckTraceCompatibilityReportsPerField(t *testing.T) {
	captured := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	baseline := TraceConditions{WavelengthNM: 1550, PulseWidthNS: 100, SampleIntervalNS: 0.5, CapturedAt: captured}
	current := TraceConditions{WavelengthNM: 1550, PulseWidthNS: 100, SampleIntervalNS: 0.5, CapturedAt: captured.Add(time.Hour)}
	result := CheckTraceCompatibility(baseline, current)
	wantFields := []string{"wavelength_nm", "pulse_width_ns", "sample_interval_ns", "capture_order"}
	for i, field := range wantFields {
		if result.Checks[i].Field != field {
			t.Fatalf("check %d field = %s, want %s", i, result.Checks[i].Field, field)
		}
		if !result.Checks[i].Compatible {
			t.Fatalf("check %s must pass for identical conditions", field)
		}
	}
}

func TestCompareBaselineTable(t *testing.T) {
	tests := []struct {
		name              string
		baseline, current []ComparableEvent
		tolerance, loss   float64
		kinds             []string
	}{
		{"new event", []ComparableEvent{{ID: 1, DistanceM: 100, InsertionLossDB: .2, Confidence: .9}}, []ComparableEvent{{ID: 2, DistanceM: 101, InsertionLossDB: .2, Confidence: .9}, {ID: 3, DistanceM: 300, InsertionLossDB: 1.1, Confidence: .8}}, 10, .5, []string{"new"}},
		{"disappeared event", []ComparableEvent{{ID: 1, DistanceM: 100, InsertionLossDB: .2, Confidence: .9}}, []ComparableEvent{{ID: 2, DistanceM: 300, InsertionLossDB: .2, Confidence: .9}}, 10, .5, []string{"disappeared", "new"}},
		{"loss increase", []ComparableEvent{{ID: 1, DistanceM: 100, InsertionLossDB: .2, Confidence: .9}}, []ComparableEvent{{ID: 2, DistanceM: 102, InsertionLossDB: 1.0, Confidence: .8}}, 10, .5, []string{"loss_increased"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			differences, err := CompareBaseline(test.baseline, test.current, test.tolerance, test.loss)
			if err != nil {
				t.Fatalf("compare: %v", err)
			}
			if len(differences) != len(test.kinds) {
				t.Fatalf("got %d differences, want %d: %#v", len(differences), len(test.kinds), differences)
			}
			for i, kind := range test.kinds {
				if differences[i].Kind != kind {
					t.Fatalf("difference %d kind = %s, want %s", i, differences[i].Kind, kind)
				}
			}
		})
	}
}
