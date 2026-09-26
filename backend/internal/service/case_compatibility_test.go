package service

import (
	"errors"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"fiber-otdr-fault-localization/backend/internal/dto"
	"fiber-otdr-fault-localization/backend/internal/model"
	"fiber-otdr-fault-localization/backend/internal/repository"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var compatibilityDB atomic.Int64

func conditionsTrace(id uint, captured time.Time) traceConditions {
	return traceConditions{
		TraceID:          id,
		RouteID:          1,
		WavelengthNM:     1550,
		PulseWidthNS:     100,
		SampleIntervalNS: 10,
		CapturedAt:       captured,
	}
}

func TestEvaluateConditions(t *testing.T) {
	earlier := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	later := earlier.Add(2 * time.Hour)
	cases := []struct {
		name     string
		baseline traceConditions
		current  traceConditions
	}{
		{
			name:     "matching conditions with newer capture",
			baseline: conditionsTrace(1, earlier),
			current:  conditionsTrace(2, later),
		},
		{
			name:     "wavelength mismatch",
			baseline: conditionsTrace(1, earlier),
			current:  func() traceConditions { c := conditionsTrace(2, later); c.WavelengthNM = 1310; return c }(),
		},
		{
			name:     "pulse width mismatch",
			baseline: conditionsTrace(1, earlier),
			current:  func() traceConditions { c := conditionsTrace(2, later); c.PulseWidthNS = 300; return c }(),
		},
		{
			name:     "sample interval mismatch",
			baseline: conditionsTrace(1, earlier),
			current:  func() traceConditions { c := conditionsTrace(2, later); c.SampleIntervalNS = 20; return c }(),
		},
		{
			name:     "current predates baseline",
			baseline: conditionsTrace(1, later),
			current:  conditionsTrace(2, earlier),
		},
		{
			name:     "identical capture time is rejected",
			baseline: conditionsTrace(1, earlier),
			current:  conditionsTrace(2, earlier),
		},
		{
			name:     "1550 baseline against 1310 current also fails order",
			baseline: conditionsTrace(1, later),
			current:  func() traceConditions { c := conditionsTrace(2, earlier); c.WavelengthNM = 1310; return c }(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checks := evaluateConditions(tc.baseline, tc.current)
			if len(checks) != 4 {
				t.Fatalf("expected four checks, got %d", len(checks))
			}
			got := map[string]bool{}
			for _, check := range checks {
				if check.Field == "" || check.Expected == "" || check.Actual == "" || check.Detail == "" {
					t.Fatalf("check %s must explain every field: %+v", check.Field, check)
				}
				got[check.Field] = check.Passed
			}
			for _, field := range []string{CheckWavelength, CheckPulseWidth, CheckSampleInterval, CheckCapturedAfter} {
				if _, ok := got[field]; !ok {
					t.Fatalf("missing check %s", field)
				}
			}
			expected := tc.baseline.WavelengthNM == tc.current.WavelengthNM
			if got[CheckWavelength] != expected {
				t.Fatalf("wavelength verdict = %v, want %v", got[CheckWavelength], expected)
			}
			expected = tc.baseline.PulseWidthNS == tc.current.PulseWidthNS
			if got[CheckPulseWidth] != expected {
				t.Fatalf("pulse width verdict = %v, want %v", got[CheckPulseWidth], expected)
			}
			expected = tc.baseline.SampleIntervalNS == tc.current.SampleIntervalNS
			if got[CheckSampleInterval] != expected {
				t.Fatalf("sample interval verdict = %v, want %v", got[CheckSampleInterval], expected)
			}
			expected = tc.current.CapturedAt.After(tc.baseline.CapturedAt)
			if got[CheckCapturedAfter] != expected {
				t.Fatalf("capture order verdict = %v, want %v", got[CheckCapturedAfter], expected)
			}
		})
	}
}

func TestCheckCompatibilityService(t *testing.T) {
	svc, store := newCompatibilityService(t)
	earlier := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	later := earlier.Add(3 * time.Hour)
	baseline := storeTrace(t, store, model.TraceCapture{RouteID: 1, WavelengthNM: 1550, PulseWidthNS: 100, SampleIntervalNS: 10, RawPointsJSON: datatypes.JSON([]byte("[]")), CapturedAt: earlier})
	current := storeTrace(t, store, model.TraceCapture{RouteID: 1, WavelengthNM: 1310, PulseWidthNS: 300, SampleIntervalNS: 10, RawPointsJSON: datatypes.JSON([]byte("[]")), CapturedAt: later})
	otherRoute := storeTrace(t, store, model.TraceCapture{RouteID: 2, WavelengthNM: 1550, PulseWidthNS: 100, SampleIntervalNS: 10, RawPointsJSON: datatypes.JSON([]byte("[]")), CapturedAt: later})

	resp, err := svc.CheckCompatibility(dto.CaseCompatibilityRequest{RouteID: 1, BaselineTraceID: baseline.ID, CurrentTraceID: current.ID})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Compatible {
		t.Fatal("1310 vs 1550 pair must not be compatible")
	}
	if resp.Baseline.WavelengthNM != 1550 || resp.Current.WavelengthNM != 1310 {
		t.Fatalf("unexpected measurement conditions: %+v", resp)
	}
	failedFields := map[string]bool{}
	for _, check := range resp.Checks {
		if !check.Passed {
			failedFields[check.Field] = true
		}
	}
	if !failedFields[CheckWavelength] || !failedFields[CheckPulseWidth] {
		t.Fatalf("expected wavelength and pulse failures, got %+v", failedFields)
	}
	if failedFields[CheckSampleInterval] || failedFields[CheckCapturedAfter] {
		t.Fatalf("sampling and order should pass, got %+v", failedFields)
	}

	_, err = svc.CheckCompatibility(dto.CaseCompatibilityRequest{RouteID: 1, BaselineTraceID: baseline.ID, CurrentTraceID: otherRoute.ID})
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Code != CodeInvalidInput {
		t.Fatalf("cross-route pair must be rejected as invalid input, got %v", err)
	}

	_, err = svc.CheckCompatibility(dto.CaseCompatibilityRequest{RouteID: 1, BaselineTraceID: baseline.ID, CurrentTraceID: 99999})
	if !errors.As(err, &appErr) || appErr.Code != CodeInvalidInput {
		t.Fatalf("missing trace must be rejected as invalid input, got %v", err)
	}
}

func TestCreateRejectsIncompatibleConditions(t *testing.T) {
	svc, store := newCompatibilityService(t)
	earlier := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	baseline := storeTrace(t, store, model.TraceCapture{RouteID: 1, WavelengthNM: 1550, PulseWidthNS: 100, SampleIntervalNS: 10, RawPointsJSON: datatypes.JSON([]byte("[]")), CapturedAt: earlier})
	current := storeTrace(t, store, model.TraceCapture{RouteID: 1, WavelengthNM: 1550, PulseWidthNS: 100, SampleIntervalNS: 20, RawPointsJSON: datatypes.JSON([]byte("[]")), CapturedAt: earlier})

	_, err := svc.Create(dto.CreateCaseRequest{RouteID: 1, BaselineTraceID: baseline.ID, CurrentTraceID: current.ID}, Actor{ID: 7, Username: "analyst", Role: "analyst"})
	var appErr *AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %v", err)
	}
	if appErr.Code != CodeConditionsDiffer || appErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("unexpected rejection: code=%s status=%d", appErr.Code, appErr.Status)
	}
	details, ok := appErr.Details.([]dto.CaseCompatibilityCheck)
	if !ok || len(details) != 2 {
		t.Fatalf("expected two failing checks (interval and capture order), got %#v", appErr.Details)
	}
	for _, detail := range details {
		if detail.Passed {
			t.Fatalf("rejection details must only list failed checks: %+v", detail)
		}
	}
	var count int64
	if err := store.DB.Model(&model.LocalizationCase{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("no case row may be created on rejection, count=%d err=%v", count, err)
	}
}

func TestCreateAcceptsCompatibleConditions(t *testing.T) {
	svc, store := newCompatibilityService(t)
	earlier := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	baseline := storeTrace(t, store, model.TraceCapture{RouteID: 1, WavelengthNM: 1310, PulseWidthNS: 50, SampleIntervalNS: 5, RawPointsJSON: datatypes.JSON([]byte("[]")), CapturedAt: earlier})
	current := storeTrace(t, store, model.TraceCapture{RouteID: 1, WavelengthNM: 1310, PulseWidthNS: 50, SampleIntervalNS: 5, RawPointsJSON: datatypes.JSON([]byte("[]")), CapturedAt: earlier.Add(time.Hour)})

	item, err := svc.Create(dto.CreateCaseRequest{RouteID: 1, BaselineTraceID: baseline.ID, CurrentTraceID: current.ID}, Actor{ID: 7, Username: "analyst", Role: "analyst", RequestID: "req-test-compatible"})
	if err != nil {
		t.Fatalf("compatible pair must create a case: %v", err)
	}
	if item.ID == 0 || item.BaselineTraceID != baseline.ID || item.CurrentTraceID != current.ID {
		t.Fatalf("unexpected case: %+v", item)
	}
}

func newCompatibilityService(t *testing.T) (*CaseService, *repository.Store) {
	t.Helper()
	dsn := "file:case-compat-" + strconv.FormatInt(compatibilityDB.Add(1), 10) + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.FiberRoute{}, &model.TraceCapture{}, &model.LocalizationCase{}, &model.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	store := repository.NewStore(db)
	routes := []model.FiberRoute{
		{RouteCode: "R-001", Name: "first", LengthM: 5000, RefractiveIndex: 1.468, LaunchConnector: "SC/APC", RouteStatus: "active"},
		{RouteCode: "R-002", Name: "second", LengthM: 5000, RefractiveIndex: 1.468, LaunchConnector: "SC/APC", RouteStatus: "active"},
	}
	for i := range routes {
		if err := db.Create(&routes[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	return NewCaseService(store), store
}

func storeTrace(t *testing.T, store *repository.Store, trace model.TraceCapture) model.TraceCapture {
	t.Helper()
	if err := store.Traces.Create(&trace); err != nil {
		t.Fatal(err)
	}
	return trace
}
