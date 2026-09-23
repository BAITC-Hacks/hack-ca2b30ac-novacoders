package httptransport

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/demo"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/recommendation"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/service"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/store"
)

func canonicalImport(t *testing.T, h http.Handler) importDTO {
	t.Helper()
	files, err := demo.Workbooks()
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for field, data := range files {
		part, err := form.CreateFormFile(uploadFields[field], demo.Filenames[field])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.WriteField("supplier", "SystemElectric"); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/v1/imports", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatal("CORS missing")
	}
	var out importDTO
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCanonicalImportAnalyzeApproveExport(t *testing.T) {
	requests := make(chan recommendation.Request, 2)
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/recommendations" || r.Method != "POST" {
			t.Errorf("incorrect AI route: %s", r.URL.Path)
		}
		var req recommendation.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		requests <- req
		rows := []any{}
		for n, p := range req.Products {
			action, quantity := "BUY", 10
			if n == 1 {
				action, quantity = "NO_BUY", 0
			}
			if n == 2 {
				action, quantity = "REVIEW", 0
			}
			if n == 3 {
				action, quantity = "REVIEW", 40
			}
			candidates := []any{}
			if n == 3 {
				candidates = append(candidates, map[string]any{"transactionId": "SALE", "date": "2026-05-14", "quantity": 420, "nvidiaVerdict": "ONE_OFF", "systemDecision": "PENDING_REVIEW", "confidence": .8, "reason": "Крупная операция"})
			}
			rows = append(rows, map[string]any{"code1C": p.Code1C, "action": action, "urgency": "HIGH", "recommendedQuantity": quantity, "estimatedUnitCost": 1000, "estimatedCost": quantity * 1000, "calculation": map[string]any{"roundedRequirement": quantity, "moq": p.MOQ, "freeStock": 10, "inTransit": 20, "availableStock": 30, "forecastDemand": 40}, "explanation": map[string]any{"short": "Расчёт сервиса", "details": []string{"Учтены остатки"}, "generatedBy": "TEMPLATE"}, "anomalyAnalysis": map[string]any{"status": "PENDING_REVIEW", "candidates": candidates}})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"schemaVersion": "1.0", "requestId": req.RequestID, "runId": "AI-run", "status": "COMPLETED_WITH_WARNINGS", "generatedAt": "2026-09-23T10:30:00Z", "processingTimeMs": 1, "recommendations": rows, "providers": map[string]any{"openai": map[string]any{"status": "FALLBACK"}}, "warnings": []any{map[string]any{"code": "OPENAI_UNAVAILABLE", "severity": "WARNING", "message": "Использовано шаблонное объяснение"}}})
	}))
	defer ai.Close()
	client, err := recommendation.NewClient(ai.URL)
	if err != nil {
		t.Fatal(err)
	}
	svc := &service.Service{Store: store.New(), Recommender: client}
	h := New(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), []string{"http://localhost:5173"})
	imported := canonicalImport(t, h)
	if !strings.HasPrefix(imported.ImportID, "import-") || imported.Status != "READY" || len(imported.Files) != 6 || imported.Summary.ProductsFound != 4 || imported.Summary.ProductsReady+imported.Summary.ProductsWithWarnings != 4 {
		t.Fatalf("%+v", imported)
	}
	for _, f := range imported.Files {
		if f.FileName == "" || f.Rows == 0 {
			t.Fatalf("%+v", f)
		}
	}
	path := "/api/v1/imports/" + imported.ImportID + "/recommendations"
	params := `{"forecastHorizonMonths":3,"leadTimeDays":45,"safetyStockDays":0,"excludePartialMonth":false}`
	w := request(h, "POST", path, params)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	sent := <-requests
	if sent.RequestID == "" || sent.Settings.ForecastHorizonMonths != 3 || sent.Settings.LeadTimeDays != 45 || sent.Settings.SafetyStockDays != 0 || sent.Settings.ExcludePartialMonth {
		t.Fatalf("settings changed: %+v", sent.Settings)
	}
	var run runDTO
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.Summary.Buy != 1 || run.Summary.NoBuy != 1 || run.Summary.Review != 2 || run.OrderSummary.Positions != 0 || run.Status != "COMPLETED_WITH_WARNINGS" || len(run.Warnings) != 1 || !strings.Contains(string(run.Providers), "FALLBACK") {
		t.Fatalf("%+v", run)
	}
	runPath := "/api/v1/recommendations/" + run.RunID
	export := func(want int) {
		t.Helper()
		w := request(h, "POST", runPath+"/export", "")
		if w.Code != 200 || w.Header().Get("Content-Type") != "text/csv; charset=utf-8" || w.Header().Get("Content-Disposition") != `attachment; filename="supplier-order.csv"` {
			t.Fatalf("export headers: %d %v", w.Code, w.Header())
		}
		rows, err := csv.NewReader(w.Body).ReadAll()
		if err != nil || len(rows) != want {
			t.Fatalf("export: %v %v", rows, err)
		}
		if want > 1 && (rows[1][1] != "030200131_" || rows[1][4] != "25") {
			t.Fatalf("wrong approved order: %v", rows)
		}
	}
	read := func() runDTO {
		t.Helper()
		w := request(h, "GET", runPath, "")
		var out runDTO
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal(w.Body.String())
		}
		return out
	}
	patch := func(body string) {
		t.Helper()
		w := request(h, "PATCH", runPath+"/items/030200131_", body)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var response map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &response)
		if response["code1C"] != "030200131_" || response["updatedAt"] == nil || response["approved"] == nil {
			t.Fatal(response)
		}
	}
	export(1)                        // BUY alone is never an approval.
	patch(`{"approvedQuantity":25}`) // A draft is not an approval either.
	if read().Items[3].Approved {
		t.Fatal("draft approved automatically")
	}
	export(1)
	patch(`{"approvedQuantity":25,"approved":true,"comment":"Проверено"}`)
	got := read()
	if got.OrderSummary.Positions != 1 || got.OrderSummary.TotalUnits != 25 || *got.OrderSummary.EstimatedCost != 25000 || got.Items[3].Decision != "REVIEW" {
		t.Fatalf("%+v", got.OrderSummary)
	}
	export(2)
	patch(`{"comment":"Другой комментарий"}`)
	export(2)
	manualWarnings := 0
	for _, warning := range read().Items[3].Warnings {
		if warning.Code == "MANAGER_QUANTITY_NOT_MULTIPLE" {
			manualWarnings++
		}
	}
	if manualWarnings != 1 {
		t.Fatal("comment edit duplicated quantity warnings")
	}
	patch(`{"anomalyDecision":"EXCLUDED"}`)
	got = read()
	if got.Items[3].Approved || got.Items[3].AnomalyDecision != "EXCLUDED" || got.Items[3].RecommendedQuantity != 40 {
		t.Fatal("anomaly decision did not invalidate approval or changed forecast")
	}
	export(1)
	patch(`{"approvedQuantity":0,"approved":true}`)
	got = read()
	if !got.Items[3].Approved || got.Items[3].ApprovedQuantity == nil || *got.Items[3].ApprovedQuantity != 0 {
		t.Fatal("explicit zero lost")
	}
	export(1)
	patch(`{"approvedQuantity":25,"approved":true}`)
	patch(`{"approved":false}`)
	export(1)
	for _, warning := range read().Items[3].Warnings {
		if warning.Code == "MANAGER_QUANTITY_NOT_MULTIPLE" {
			t.Fatal("withdrawn approval left a stale quantity warning")
		}
	}
	for _, bad := range []string{`{}`, `{"approved":true}`, `{"approvedQuantity":-1,"approved":true}`, `{"anomalyDecision":"EXCLUDE"}`} {
		if w := request(h, "PATCH", runPath+"/items/030200131_", bad); w.Code != 400 {
			t.Fatalf("accepted %s: %d", bad, w.Code)
		}
	}
	if w := request(h, "POST", path, `{"forecastHorizonMonths":0,"leadTimeDays":30,"safetyStockDays":14,"excludePartialMonth":true}`); w.Code != 400 {
		t.Fatal("invalid settings accepted")
	}
	var wg sync.WaitGroup
	for n := range 12 {
		wg.Go(func() {
			var w *httptest.ResponseRecorder
			if n%2 == 0 {
				w = request(h, "PATCH", runPath+"/items/030200131_", `{"approvedQuantity":25,"approved":true}`)
			} else {
				w = request(h, "GET", runPath, "")
			}
			if w.Code != 200 {
				t.Errorf("concurrent update: %d", w.Code)
			}
		})
	}
	wg.Wait()
	ai.Close()
	if w := request(h, "POST", path, params); w.Code != 503 || !strings.Contains(w.Body.String(), "AI_SERVICE_UNAVAILABLE") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w := request(h, "GET", "/api/v1/imports/"+imported.ImportID, ""); w.Code != 200 {
		t.Fatal("dataset lost after AI outage")
	}
	if _, err := svc.Store.Run("run-002"); err == nil {
		t.Fatal("fake run created on outage")
	}
}
