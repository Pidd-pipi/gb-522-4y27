package service

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"fiber-otdr-fault-localization/backend/internal/dto"
	"fiber-otdr-fault-localization/backend/internal/model"
	"fiber-otdr-fault-localization/backend/internal/repository"
)

// Measurement-condition check field identifiers shared by the precheck
// endpoint and the rejection payload on case creation.
const (
	CheckWavelength     = "wavelength_nm"
	CheckPulseWidth     = "pulse_width_ns"
	CheckSampleInterval = "sample_interval_ns"
	CheckCapturedAfter  = "captured_after_baseline"
)

// traceConditions is the minimum trace data required to judge whether a
// baseline/current pair can be compared.
type traceConditions struct {
	TraceID          uint
	RouteID          uint
	WavelengthNM     int
	PulseWidthNS     float64
	SampleIntervalNS float64
	CapturedAt       time.Time
}

// compatibilityCheck is the service-layer verdict for one condition.
type compatibilityCheck struct {
	Field    string
	Passed   bool
	Expected string
	Actual   string
	Detail   string
}

func (s *CaseService) CheckCompatibility(request dto.CaseCompatibilityRequest) (dto.CaseCompatibilityResponse, error) {
	baseline, current, err := s.loadCaseTraces(request.RouteID, request.BaselineTraceID, request.CurrentTraceID)
	if err != nil {
		return dto.CaseCompatibilityResponse{}, err
	}
	return buildCompatibilityResponse(baseline, current), nil
}

// loadCaseTraces loads both traces and enforces the route membership that
// the baseline review is responsible for today.
func (s *CaseService) loadCaseTraces(routeID, baselineID, currentID uint) (traceConditions, traceConditions, error) {
	ids := []uint{baselineID, currentID}
	traces := make([]traceConditions, 2)
	for i, traceID := range ids {
		trace, err := s.store.Traces.Get(traceID)
		if errors.Is(err, repository.ErrNotFound) {
			return traceConditions{}, traceConditions{}, invalid("both traces must belong to the selected route", nil)
		}
		if err != nil {
			return traceConditions{}, traceConditions{}, internal("validate case traces failed", err)
		}
		if trace.RouteID != routeID {
			return traceConditions{}, traceConditions{}, invalid("both traces must belong to the selected route", nil)
		}
		traces[i] = traceConditionsFromModel(trace)
	}
	return traces[0], traces[1], nil
}

func traceConditionsFromModel(trace model.TraceCapture) traceConditions {
	return traceConditions{
		TraceID:          trace.ID,
		RouteID:          trace.RouteID,
		WavelengthNM:     trace.WavelengthNM,
		PulseWidthNS:     trace.PulseWidthNS,
		SampleIntervalNS: trace.SampleIntervalNS,
		CapturedAt:       trace.CapturedAt,
	}
}

func buildCompatibilityResponse(baseline, current traceConditions) dto.CaseCompatibilityResponse {
	checks := evaluateConditions(baseline, current)
	compatible := true
	dtoChecks := make([]dto.CaseCompatibilityCheck, 0, len(checks))
	for _, check := range checks {
		if !check.Passed {
			compatible = false
		}
		dtoChecks = append(dtoChecks, dto.CaseCompatibilityCheck{Field: check.Field, Passed: check.Passed, Expected: check.Expected, Actual: check.Actual, Detail: check.Detail})
	}
	return dto.CaseCompatibilityResponse{
		Compatible: compatible,
		Baseline:   measurementDTO(baseline),
		Current:    measurementDTO(current),
		Checks:     dtoChecks,
	}
}

func measurementDTO(trace traceConditions) dto.TraceMeasurement {
	return dto.TraceMeasurement{
		TraceID:          trace.TraceID,
		RouteID:          trace.RouteID,
		WavelengthNM:     trace.WavelengthNM,
		PulseWidthNS:     trace.PulseWidthNS,
		SampleIntervalNS: trace.SampleIntervalNS,
		CapturedAt:       trace.CapturedAt,
	}
}

// evaluateConditions judges wavelength, pulse width, sampling interval and
// acquisition order. The first three must match exactly because OTDR
// backscatter levels, event widths and distance grids are not comparable
// across different acquisition settings.
func evaluateConditions(baseline, current traceConditions) []compatibilityCheck {
	wavelengthOK := baseline.WavelengthNM == current.WavelengthNM
	wavelength := compatibilityCheck{
		Field:    CheckWavelength,
		Passed:   wavelengthOK,
		Expected: strconv.Itoa(baseline.WavelengthNM) + " nm (baseline)",
		Actual:   fmt.Sprintf("baseline %d nm / current %d nm", baseline.WavelengthNM, current.WavelengthNM),
	}
	if wavelengthOK {
		wavelength.Detail = fmt.Sprintf("both traces were acquired at %d nm", baseline.WavelengthNM)
	} else {
		wavelength.Detail = fmt.Sprintf("wavelength differs: baseline %d nm vs current %d nm; backscatter loss cannot be compared across wavelengths", baseline.WavelengthNM, current.WavelengthNM)
	}

	pulseOK := baseline.PulseWidthNS == current.PulseWidthNS
	pulse := compatibilityCheck{
		Field:    CheckPulseWidth,
		Passed:   pulseOK,
		Expected: fmt.Sprintf("%.4g ns (baseline)", baseline.PulseWidthNS),
		Actual:   fmt.Sprintf("baseline %.4g ns / current %.4g ns", baseline.PulseWidthNS, current.PulseWidthNS),
	}
	if pulseOK {
		pulse.Detail = fmt.Sprintf("both traces used a %.4g ns pulse", baseline.PulseWidthNS)
	} else {
		pulse.Detail = fmt.Sprintf("pulse width differs: baseline %.4g ns vs current %.4g ns; resolution and event loss are not comparable", baseline.PulseWidthNS, current.PulseWidthNS)
	}

	intervalOK := baseline.SampleIntervalNS == current.SampleIntervalNS
	interval := compatibilityCheck{
		Field:    CheckSampleInterval,
		Passed:   intervalOK,
		Expected: fmt.Sprintf("%.4g ns (baseline)", baseline.SampleIntervalNS),
		Actual:   fmt.Sprintf("baseline %.4g ns / current %.4g ns", baseline.SampleIntervalNS, current.SampleIntervalNS),
	}
	if intervalOK {
		interval.Detail = fmt.Sprintf("both traces were sampled every %.4g ns", baseline.SampleIntervalNS)
	} else {
		interval.Detail = fmt.Sprintf("sample interval differs: baseline %.4g ns vs current %.4g ns; distance grids are not aligned", baseline.SampleIntervalNS, current.SampleIntervalNS)
	}

	timeOK := current.CapturedAt.After(baseline.CapturedAt)
	order := compatibilityCheck{
		Field:    CheckCapturedAfter,
		Passed:   timeOK,
		Expected: "current captured later than " + baseline.CapturedAt.Format(time.RFC3339),
		Actual:   fmt.Sprintf("baseline %s / current %s", baseline.CapturedAt.Format(time.RFC3339), current.CapturedAt.Format(time.RFC3339)),
	}
	if timeOK {
		order.Detail = "the current trace was acquired after the baseline trace"
	} else if current.CapturedAt.Equal(baseline.CapturedAt) {
		order.Detail = "both traces report the same acquisition time; the current trace must be newer than the baseline"
	} else {
		order.Detail = fmt.Sprintf("current trace (%s) predates the baseline (%s); the baseline must be the older capture", current.CapturedAt.Format(time.RFC3339), baseline.CapturedAt.Format(time.RFC3339))
	}

	return []compatibilityCheck{wavelength, pulse, interval, order}
}

// incompatibleConditionsError rejects case creation with a per-condition
// explanation so callers can show exactly what mismatched.
func incompatibleConditionsError(response dto.CaseCompatibilityResponse) error {
	failed := make([]dto.CaseCompatibilityCheck, 0, len(response.Checks))
	for _, check := range response.Checks {
		if !check.Passed {
			failed = append(failed, check)
		}
	}
	return &AppError{
		Code:    CodeConditionsDiffer,
		Status:  http.StatusUnprocessableEntity,
		Message: "measurement conditions are not comparable; case creation was rejected",
		Details: failed,
	}
}
