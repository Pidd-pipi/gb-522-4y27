package algorithm

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ComparableEvent struct {
	ID                                     uint
	DistanceM, InsertionLossDB, Confidence float64
}

type Difference struct {
	Kind            string  `json:"kind"`
	BaselineEventID *uint   `json:"baseline_event_id,omitempty"`
	CurrentEventID  *uint   `json:"current_event_id,omitempty"`
	DistanceM       float64 `json:"distance_m"`
	LossDeltaDB     float64 `json:"loss_delta_db"`
	Confidence      float64 `json:"confidence"`
}

func CompareBaseline(baseline, current []ComparableEvent, toleranceM, lossIncreaseDB float64) ([]Difference, error) {
	if toleranceM <= 0 {
		return nil, fmt.Errorf("distance tolerance must be positive")
	}
	if lossIncreaseDB <= 0 {
		return nil, fmt.Errorf("loss increase threshold must be positive")
	}
	used := make(map[int]bool)
	differences := make([]Difference, 0)
	for _, base := range baseline {
		match := -1
		best := math.MaxFloat64
		for i, candidate := range current {
			if used[i] {
				continue
			}
			delta := math.Abs(base.DistanceM - candidate.DistanceM)
			if delta <= toleranceM && delta < best {
				match, best = i, delta
			}
		}
		if match < 0 {
			id := base.ID
			differences = append(differences, Difference{Kind: "disappeared", BaselineEventID: &id, DistanceM: base.DistanceM, Confidence: round(base.Confidence * 0.9)})
			continue
		}
		used[match] = true
		candidate := current[match]
		lossDelta := candidate.InsertionLossDB - base.InsertionLossDB
		if lossDelta >= lossIncreaseDB {
			baseID, currentID := base.ID, candidate.ID
			differences = append(differences, Difference{Kind: "loss_increased", BaselineEventID: &baseID, CurrentEventID: &currentID, DistanceM: candidate.DistanceM, LossDeltaDB: round(lossDelta), Confidence: round(math.Min(base.Confidence, candidate.Confidence))})
		}
	}
	for i, candidate := range current {
		if used[i] {
			continue
		}
		id := candidate.ID
		differences = append(differences, Difference{Kind: "new", CurrentEventID: &id, DistanceM: candidate.DistanceM, LossDeltaDB: candidate.InsertionLossDB, Confidence: round(candidate.Confidence)})
	}
	sort.Slice(differences, func(i, j int) bool {
		if differences[i].DistanceM == differences[j].DistanceM {
			return differences[i].Kind < differences[j].Kind
		}
		return differences[i].DistanceM < differences[j].DistanceM
	})
	return differences, nil
}

func PrimaryDifference(differences []Difference) (distance, uncertainty float64, ok bool) {
	if len(differences) == 0 {
		return 0, 0, false
	}
	best := differences[0]
	for _, difference := range differences[1:] {
		if difference.Confidence > best.Confidence || difference.Kind == "loss_increased" && best.Kind != "loss_increased" {
			best = difference
		}
	}
	return best.DistanceM, math.Max(1, 20*(1-best.Confidence)), true
}

// TraceConditions captures the acquisition parameters that decide whether two
// traces may be compared at all.
type TraceConditions struct {
	WavelengthNM     int
	PulseWidthNS     float64
	SampleIntervalNS float64
	CapturedAt       time.Time
}

// CompatibilityCheck reports one applicability criterion for a trace pair.
type CompatibilityCheck struct {
	Field         string `json:"field"`
	BaselineValue string `json:"baseline_value"`
	CurrentValue  string `json:"current_value"`
	Compatible    bool   `json:"compatible"`
}

// CompatibilityResult aggregates the per-field applicability checks.
type CompatibilityResult struct {
	Checks     []CompatibilityCheck `json:"checks"`
	Compatible bool                 `json:"compatible"`
}

// CheckTraceCompatibility verifies that two captures share wavelength, pulse
// width, and sampling interval, and that the current capture is newer than the
// baseline. Traces recorded under different conditions are not comparable:
// equal sample positions would map to different fiber locations or resolutions.
func CheckTraceCompatibility(baseline, current TraceConditions) CompatibilityResult {
	checks := []CompatibilityCheck{
		{Field: "wavelength_nm", BaselineValue: strconv.Itoa(baseline.WavelengthNM), CurrentValue: strconv.Itoa(current.WavelengthNM), Compatible: baseline.WavelengthNM == current.WavelengthNM},
		{Field: "pulse_width_ns", BaselineValue: formatCondition(baseline.PulseWidthNS), CurrentValue: formatCondition(current.PulseWidthNS), Compatible: baseline.PulseWidthNS == current.PulseWidthNS},
		{Field: "sample_interval_ns", BaselineValue: formatCondition(baseline.SampleIntervalNS), CurrentValue: formatCondition(current.SampleIntervalNS), Compatible: baseline.SampleIntervalNS == current.SampleIntervalNS},
		{Field: "capture_order", BaselineValue: baseline.CapturedAt.UTC().Format(time.RFC3339), CurrentValue: current.CapturedAt.UTC().Format(time.RFC3339), Compatible: current.CapturedAt.After(baseline.CapturedAt)},
	}
	result := CompatibilityResult{Checks: checks, Compatible: true}
	for _, check := range checks {
		result.Compatible = result.Compatible && check.Compatible
	}
	return result
}

// FailureSummary explains every failed check, one clause per item.
func (r CompatibilityResult) FailureSummary() string {
	clauses := make([]string, 0, len(r.Checks))
	for _, check := range r.Checks {
		if check.Compatible {
			continue
		}
		switch check.Field {
		case "wavelength_nm":
			clauses = append(clauses, fmt.Sprintf("wavelength differs (baseline %s nm, current %s nm)", check.BaselineValue, check.CurrentValue))
		case "pulse_width_ns":
			clauses = append(clauses, fmt.Sprintf("pulse width differs (baseline %s ns, current %s ns)", check.BaselineValue, check.CurrentValue))
		case "sample_interval_ns":
			clauses = append(clauses, fmt.Sprintf("sample interval differs (baseline %s ns, current %s ns)", check.BaselineValue, check.CurrentValue))
		default:
			clauses = append(clauses, fmt.Sprintf("current trace captured at %s is not later than the baseline capture at %s", check.CurrentValue, check.BaselineValue))
		}
	}
	return strings.Join(clauses, "; ")
}

func formatCondition(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
