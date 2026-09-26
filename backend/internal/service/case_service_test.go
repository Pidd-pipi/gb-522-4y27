package service

import (
	"strings"
	"testing"
	"time"

	"fiber-otdr-fault-localization/backend/internal/constants"
	"fiber-otdr-fault-localization/backend/internal/dto"
	"fiber-otdr-fault-localization/backend/internal/model"
	"fiber-otdr-fault-localization/backend/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupCaseService(t *testing.T) (*CaseService, *repository.Store, model.FiberRoute) {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.FiberRoute{}, &model.TraceCapture{}, &model.LocalizationCase{}, &model.AuditLog{}); err != nil {
		t.Fatal(err)
	}
	store := repository.NewStore(db)
	route := model.FiberRoute{RouteCode: "CASET1", Name: "case service test route", LengthM: 5000, RefractiveIndex: 1.468, LaunchConnector: "SC/UPC", RouteStatus: "active"}
	if err := db.Create(&route).Error; err != nil {
		t.Fatal(err)
	}
	return NewCaseService(store), store, route
}

func seedTrace(t *testing.T, store *repository.Store, routeID uint, wavelength int, capturedAt time.Time) model.TraceCapture {
	t.Helper()
	trace := model.TraceCapture{RouteID: routeID, WavelengthNM: wavelength, PulseWidthNS: 100, SampleIntervalNS: 0.5, RawPointsJSON: []byte("[]"), NoiseFloorDB: -32, DenoiseWindow: 5, PeakThresholdDB: 0.8, MergeWindow: 3, CapturedAt: capturedAt, UploadedBy: 1}
	if err := store.Traces.Create(&trace); err != nil {
		t.Fatal(err)
	}
	return trace
}

func caseActor() Actor {
	return Actor{ID: 1, Username: "analyst", Role: constants.RoleAnalyst, RequestID: "req-case-test"}
}

func TestCreateCaseRejectsMismatchedWavelength(t *testing.T) {
	service, store, route := setupCaseService(t)
	captured := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	baseline := seedTrace(t, store, route.ID, 1550, captured)
	current := seedTrace(t, store, route.ID, 1310, captured.Add(time.Hour))
	_, err := service.Create(dto.CreateCaseRequest{RouteID: route.ID, BaselineTraceID: baseline.ID, CurrentTraceID: current.ID}, caseActor())
	appErr, ok := err.(*AppError)
	if !ok || appErr.Code != CodeInvalidInput {
		t.Fatalf("expected INVALID_INPUT, got %v", err)
	}
	for _, fragment := range []string{"wavelength", "1550", "1310"} {
		if !strings.Contains(appErr.Message, fragment) {
			t.Fatalf("rejection must itemize %q: %s", fragment, appErr.Message)
		}
	}
	var count int64
	if err := store.DB.Model(&model.LocalizationCase{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rejected case must not persist, count=%d err=%v", count, err)
	}
}

func TestCreateCaseRejectsOutOfOrderCapture(t *testing.T) {
	service, store, route := setupCaseService(t)
	captured := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	baseline := seedTrace(t, store, route.ID, 1550, captured)
	current := seedTrace(t, store, route.ID, 1550, captured.Add(-time.Hour))
	_, err := service.Create(dto.CreateCaseRequest{RouteID: route.ID, BaselineTraceID: baseline.ID, CurrentTraceID: current.ID}, caseActor())
	appErr, ok := err.(*AppError)
	if !ok || appErr.Code != CodeInvalidInput {
		t.Fatalf("expected INVALID_INPUT, got %v", err)
	}
	if !strings.Contains(appErr.Message, "not later") {
		t.Fatalf("rejection must explain capture order: %s", appErr.Message)
	}
}

func TestCreateCaseRejectsTraceFromOtherRoute(t *testing.T) {
	service, store, route := setupCaseService(t)
	other := model.FiberRoute{RouteCode: "CASET2", Name: "other route", LengthM: 3000, RefractiveIndex: 1.468, LaunchConnector: "SC/UPC", RouteStatus: "active"}
	if err := store.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	captured := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	baseline := seedTrace(t, store, route.ID, 1550, captured)
	current := seedTrace(t, store, other.ID, 1550, captured.Add(time.Hour))
	_, err := service.Create(dto.CreateCaseRequest{RouteID: route.ID, BaselineTraceID: baseline.ID, CurrentTraceID: current.ID}, caseActor())
	appErr, ok := err.(*AppError)
	if !ok || appErr.Code != CodeInvalidInput || !strings.Contains(appErr.Message, "belong to the selected route") {
		t.Fatalf("expected route membership rejection, got %v", err)
	}
}

func TestCreateCaseAcceptsCompatibleTraces(t *testing.T) {
	service, store, route := setupCaseService(t)
	captured := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	baseline := seedTrace(t, store, route.ID, 1550, captured)
	current := seedTrace(t, store, route.ID, 1550, captured.Add(time.Hour))
	item, err := service.Create(dto.CreateCaseRequest{RouteID: route.ID, BaselineTraceID: baseline.ID, CurrentTraceID: current.ID}, caseActor())
	if err != nil {
		t.Fatalf("compatible traces must be accepted: %v", err)
	}
	if item.CaseStatus != constants.CaseDraft || item.ID == 0 {
		t.Fatalf("unexpected case: id=%d status=%s", item.ID, item.CaseStatus)
	}
	var entry model.AuditLog
	if err := store.DB.Where("action = ? AND resource_id = ?", "case.created", item.ID).First(&entry).Error; err != nil {
		t.Fatalf("case.created audit missing: %v", err)
	}
	if !strings.Contains(entry.After, "wavelength_nm") {
		t.Fatalf("audit must record the applicability checks: %s", entry.After)
	}
}
