package httptransport

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/recommendation"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/service"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/store"
)

func TestUploadRemoteCalculationApprovalAndExport(t *testing.T) {
	received := make(chan recommendation.Request, 1)
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req recommendation.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		received <- req
		items := []any{}
		for n, p := range req.Products {
			qty, rounded, action := 0, 0, "NO_BUY"
			candidates := []any{}
			if n == 0 {
				qty, rounded, action = 25, 40, "BUY"
				candidates = append(candidates, map[string]any{"transactionId": "SALE", "date": "2026-09-10", "quantity": 420, "nvidiaVerdict": "ONE_OFF", "systemDecision": "PENDING_REVIEW"})
			}
			items = append(items, map[string]any{"code1C": p.Code1C, "action": action, "urgency": "HIGH", "recommendedQuantity": qty, "estimatedUnitCost": 1000, "estimatedCost": qty * 1000, "calculation": map[string]any{"roundedRequirement": rounded, "moq": p.MOQ, "freeStock": nil, "correctedMonthlyDemand": 24.33}, "anomalyAnalysis": map[string]any{"status": "PENDING_REVIEW", "candidates": candidates}, "explanation": map[string]any{"short": "Объяснение сервиса", "generatedBy": "TEMPLATE"}})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"schemaVersion": "1.0", "requestId": req.RequestID, "runId": "external-123", "status": "COMPLETED_WITH_WARNINGS", "generatedAt": "2026-09-23T10:30:00Z", "processingTimeMs": 1, "recommendations": items, "providers": map[string]any{"openai": map[string]any{"status": "FALLBACK"}}})
	}))
	defer ai.Close()
	client, err := recommendation.NewClient(ai.URL)
	if err != nil {
		t.Fatal(err)
	}
	svc := &service.Service{Store: store.New(), Recommender: client}
	h := New(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	id := importDemo(t, h)
	w := request(h, "POST", "/api/v1/runs", `{"datasetId":"`+id+`","supplier":"SystemElectric"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	req := <-received
	if req.SchemaVersion != "1.0" || req.AsOfDate != "2026-09-22" || req.Currency != "KZT" || len(req.Products) != 4 || len(req.SourceMeta) != 6 || req.SourceMeta["moq"].FileName == "" || req.Products[2].MOQ != nil || req.Settings.ForecastHorizonMonths != 2 {
		t.Fatalf("bad normalized request: %+v", req)
	}
	var run domain.CalculationRun
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.Items[0].Decision != "REVIEW" || !run.Items[0].RequiresManualReview || run.Items[0].RecommendedQuantity != 25 || run.Items[0].ApprovedQuantity != nil || run.Items[0].Breakdown != nil {
		t.Fatalf("bad recommendation: %+v", run.Items[0])
	}
	if !strings.Contains(string(run.Items[0].Calculation), `"correctedMonthlyDemand":24.33`) || !strings.Contains(string(run.Items[0].Calculation), `"freeStock":null`) || !strings.Contains(string(run.Items[0].Explanation), "Объяснение сервиса") {
		t.Fatal("AI fields not preserved")
	}
	found := false
	for _, warn := range run.Items[0].Warnings {
		if warn.Code == "AI_QUANTITY_MISMATCH" {
			found = true
		}
	}
	if !found {
		t.Fatal("inconsistent math not flagged")
	}
	raw := string(run.AIResponse)
	ai.Close() // Approval and export must work without the external service.
	base := "/api/v1/runs/" + run.ID
	assertExportRows := func(want int) {
		t.Helper()
		w := request(h, "POST", base+"/approve-export", "")
		rows, err := csv.NewReader(w.Body).ReadAll()
		if err != nil || len(rows) != want {
			t.Fatalf("%d %v %v", w.Code, rows, err)
		}
	}
	assertExportRows(1)
	for _, patch := range []string{`{"approvedQuantity":30}`, `{"comment":"Проверено"}`} {
		w = request(h, "PATCH", base+"/items/030200128_", patch)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
			t.Fatal(err)
		}
		if run.Items[0].FinalQuantity != 30 || *run.Items[0].EstimatedCost != 30000 || string(run.AIResponse) != raw || run.Summary.ApprovedItems != 1 {
			t.Fatal("approval changed AI evidence or was lost")
		}
	}
	assertExportRows(2)
	w = request(h, "PATCH", base+"/items/030200128_", `{"anomalyDecision":"EXCLUDE"}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.Items[0].ApprovedQuantity != nil || run.Items[0].AnomalyDecision != "EXCLUDE" || run.Items[0].RecommendedQuantity != 25 || *run.Items[0].EstimatedCost != 25000 || string(run.AIResponse) != raw {
		t.Fatal("decision must invalidate approval and preserve AI response")
	}
	assertExportRows(1)
	w = request(h, "POST", "/api/v1/runs", `{"datasetId":"`+id+`","supplier":"SystemElectric"}`)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "AI_SERVICE_UNAVAILABLE") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if w = request(h, "GET", "/api/v1/datasets/"+id, ""); w.Code != 200 {
		t.Fatal("dataset lost after AI failure")
	}
	if _, err := svc.Store.Run("run-002"); err == nil {
		t.Fatal("failed run stored as success")
	}
}
