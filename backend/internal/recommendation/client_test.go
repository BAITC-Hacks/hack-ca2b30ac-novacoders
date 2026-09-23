package recommendation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
)

func input(t *testing.T) *Request {
	t.Helper()
	p := &domain.Product{Code1C: "003_", Supplier: "IEK", MonthlySales: map[string]float64{"2026-06": 0, "2026-07": -3}, BlankSalesMonths: []string{"2026-08"}, MonthlyStock: map[string]*float64{"2026-06": ptr(0), "2026-07": nil}, PresentFields: map[string]bool{"inTransit": true}, Transactions: []domain.Transaction{{Date: domain.DatasetDate(), DocumentID: "SAME", Quantity: -2}, {Date: domain.DatasetDate(), DocumentID: "SAME", Quantity: 3}}}
	d := &domain.Dataset{AsOf: domain.DatasetDate(), Products: map[string]*domain.Product{p.Code1C: p}, Seasonality: map[int]float64{9: .892}, SourceFiles: map[string]string{"moq": "MOQ ИЭК.xlsx"}, Diagnostics: domain.ImportDiagnostics{ProcessedRows: map[string]int{"moq": 1}}}
	r, err := Build(d, domain.RunConfig{Supplier: "IEK"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func validReply(r *Request) map[string]any {
	return map[string]any{"schemaVersion": "1.0", "requestId": r.RequestID, "runId": "external-1", "status": "COMPLETED_WITH_WARNINGS", "generatedAt": "2026-09-23T10:30:00Z", "processingTimeMs": 12, "providers": map[string]any{"openai": map[string]any{"status": "FALLBACK"}}, "recommendations": []any{map[string]any{"code1C": "003_", "action": "REVIEW", "urgency": "HIGH", "recommendedQuantity": 0, "estimatedUnitCost": nil, "estimatedCost": nil, "calculation": map[string]any{"roundedRequirement": 0, "freeStock": nil}, "explanation": map[string]any{"generatedBy": "TEMPLATE", "short": "Проверьте остатки"}}}}
}
func TestNormalizePreservesUnknownZeroReturnsAndTransactions(t *testing.T) {
	r := input(t)
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(r.RequestID) {
		t.Fatal(r.RequestID)
	}
	if r.RequestID == input(t).RequestID {
		t.Fatal("request IDs reused")
	}
	if r.Products[0].Code1C != "003_" || r.Products[0].MOQ != nil || r.Products[0].UnitCost != nil || r.Inventory[0].FreeStock != nil || r.Inventory[0].InTransit == nil || *r.Inventory[0].InTransit != 0 {
		t.Fatalf("null/zero/code lost: %+v", r)
	}
	if len(r.Transactions) != 2 || *r.Transactions[0].Quantity != -2 || *r.Transactions[1].Quantity != 3 || r.Transactions[0].Date != "2026-09-22" {
		t.Fatal("transactions aggregated or changed")
	}
	if len(r.MonthlySales) != 3 || *r.MonthlySales[0].Quantity != 0 || *r.MonthlySales[1].Quantity != -3 || r.MonthlySales[2].Quantity != nil {
		t.Fatal("monthly values changed")
	}
	if r.MonthlyStock[0].Quantity == nil || *r.MonthlyStock[0].Quantity != 0 || r.MonthlyStock[1].Quantity != nil {
		t.Fatal("stock null/zero lost")
	}
	if r.SourceMeta["moq"].FileName != "MOQ ИЭК.xlsx" || r.Settings.ForecastHorizonMonths != 2 || r.Settings.SafetyStockDays != 14 {
		t.Fatal("metadata/defaults changed")
	}
	r.Products = nil
	r.MonthlySales = []Monthly{}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"monthlySales":[]`) {
		t.Fatal(string(data))
	}
}
func TestClientContractAndInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
		bad    bool
	}{
		{name: "fallback"},
		{name: "wrong ID", bad: true, mutate: func(m map[string]any) { m["requestId"] = "different" }},
		{name: "version", bad: true, mutate: func(m map[string]any) { m["schemaVersion"] = "2.0" }},
		{name: "failed", bad: true, mutate: func(m map[string]any) { m["status"] = "FAILED" }},
		{name: "missing product", bad: true, mutate: func(m map[string]any) { m["recommendations"] = []any{} }},
		{name: "duplicate", bad: true, mutate: func(m map[string]any) { a := m["recommendations"].([]any); m["recommendations"] = append(a, a[0]) }},
		{name: "missing quantity", bad: true, mutate: func(m map[string]any) {
			delete(m["recommendations"].([]any)[0].(map[string]any), "recommendedQuantity")
		}},
		{name: "negative quantity", bad: true, mutate: func(m map[string]any) { m["recommendations"].([]any)[0].(map[string]any)["recommendedQuantity"] = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := input(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/v1/recommendations" || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("wrong request %s %s", r.Method, r.URL.Path)
				}
				var got Request
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Error(err)
				}
				if got.RequestID != req.RequestID || len(got.Transactions) != 2 {
					t.Error("payload changed")
				}
				reply := validReply(&got)
				if tc.mutate != nil {
					tc.mutate(reply)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(reply)
			}))
			defer srv.Close()
			client, err := NewClient(srv.URL + "/")
			if err != nil {
				t.Fatal(err)
			}
			out, err := client.Recommend(context.Background(), req)
			if tc.bad {
				var e *Error
				if !errors.As(err, &e) || e.HTTPStatus != 502 {
					t.Fatalf("%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out.Raw), `"FALLBACK"`) || !strings.Contains(string(out.Raw), `"freeStock":null`) {
				t.Fatal("original response lost")
			}
		})
	}
}
func TestClientErrorStatusAndTimeout(t *testing.T) {
	for _, tc := range []struct {
		status, want int
		body, media  string
	}{
		{422, 422, `{"error":{"code":"VALIDATION_ERROR","message":"bad","fields":[{"field":"products[0].code1C","message":"missing"}]}}`, "application/json"},
		{400, 422, `{"error":{"code":"INVALID_REQUEST","message":"bad"}}`, "application/json"},
		{413, 413, `{"error":{"code":"PAYLOAD_TOO_LARGE","message":"big"}}`, "application/json"},
		{401, 502, `{"error":{"code":"UNAUTHORIZED","message":"secret"}}`, "application/json"},
		{503, 503, `{"error":{"code":"AI_NOT_CONFIGURED","message":"secret"}}`, "application/json"},
		{504, 504, `{"error":{"code":"REQUEST_TIMEOUT","message":"secret"}}`, "application/json"},
		{500, 502, `{"secret":"do not expose"}`, "application/json"},
		{200, 502, `<html>oops</html>`, "text/html"},
		{200, 502, `{} {}`, "application/json"},
		{302, 502, `{}`, "application/json"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", tc.media)
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		c, _ := NewClient(srv.URL)
		_, err := c.Recommend(context.Background(), input(t))
		srv.Close()
		var e *Error
		if !errors.As(err, &e) || e.HTTPStatus != tc.want || strings.Contains(e.Message, "secret") {
			t.Fatalf("%+v: %v", tc, err)
		}
		if tc.status == 422 && (len(e.Fields) != 1 || e.Fields[0].Field != "products[0].code1C") {
			t.Fatal("validation fields lost")
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL)
	c.http.Timeout = 20 * time.Millisecond
	_, err := c.Recommend(context.Background(), input(t))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Recommend(ctx, input(t))
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	srv.Close()
	_, err = c.Recommend(context.Background(), input(t))
	var e *Error
	if !errors.As(err, &e) || e.HTTPStatus != 503 {
		t.Fatal(err)
	}
}

func TestUnknownTransactionQuantityIsNull(t *testing.T) {
	p := &domain.Product{Code1C: "001_", Supplier: "IEK", Transactions: []domain.Transaction{{Date: domain.DatasetDate(), DocumentID: "T", QuantityMissing: true}}}
	r, err := Build(&domain.Dataset{AsOf: domain.DatasetDate(), Products: map[string]*domain.Product{p.Code1C: p}}, domain.RunConfig{Supplier: "IEK"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Transactions) != 1 || r.Transactions[0].Quantity != nil || !strings.Contains(string(data), `"quantity":null`) {
		t.Fatal("unknown transaction discarded or turned into zero")
	}
	if r.MonthlySales == nil || r.MonthlyStock == nil || r.Seasonality == nil {
		t.Fatal("empty arrays must not serialize to null")
	}
}

func TestConfiguredClientSendsInternalTokenAndHonorsDeadline(t *testing.T) {
	req := input(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer integration-test-token" {
			t.Error("missing internal authorization")
		}
		if r.Header.Get("OpenAI-Organization") != "" {
			t.Error("provider credentials do not belong in backend")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(validReply(req))
	}))
	defer srv.Close()
	c, err := NewConfiguredClient(ClientConfig{BaseURL: srv.URL, Token: "integration-test-token", ServiceDeadline: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if c.http.Timeout != 35*time.Second {
		t.Fatal("Go must allow AI deadline plus response margin")
	}
	if _, err := c.Recommend(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []ClientConfig{
		{BaseURL: srv.URL, ServiceDeadline: 0},
		{BaseURL: srv.URL, ServiceDeadline: 61 * time.Second},
		{BaseURL: srv.URL, Token: "bad\r\nheader", ServiceDeadline: time.Second},
	} {
		if _, err := NewConfiguredClient(cfg); err == nil {
			t.Fatal("invalid AI config accepted")
		}
	}
}

func TestInternalTokenNeverFollowsRedirects(t *testing.T) {
	forwarded := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer source.Close()
	c, err := NewConfiguredClient(ClientConfig{BaseURL: source.URL, Token: "integration-test-token", ServiceDeadline: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Recommend(context.Background(), input(t)); err == nil || forwarded {
		t.Fatal("redirect followed or accepted")
	}
}

func TestUnknownRecommendationReturns422WithOriginalNullResult(t *testing.T) {
	req := input(t)
	reply := validReply(req)
	item := reply["recommendations"].([]any)[0].(map[string]any)
	item["recommendedQuantity"], item["requiresManualReview"] = nil, true
	item["processingStatus"], item["missingFields"] = "needs_review", []string{"inventory.freeStock"}
	item["calculation"].(map[string]any)["roundedRequirement"] = nil
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reply)
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL)
	result, err := c.Recommend(context.Background(), req)
	var incomplete *Error
	if result != nil || !errors.As(err, &incomplete) || incomplete.HTTPStatus != 422 || incomplete.Code != "AI_INCOMPLETE_DATA" || incomplete.RequestID != req.RequestID {
		t.Fatalf("unexpected result/error: %v", err)
	}
	if len(incomplete.Fields) != 1 || !strings.Contains(incomplete.Fields[0].Message, "inventory.freeStock") {
		t.Fatal("missing fields lost")
	}
	if !strings.Contains(string(incomplete.Result), `"recommendedQuantity":null`) || !strings.Contains(string(incomplete.Result), `"explanation"`) {
		t.Fatal("null or AI explanation lost")
	}
}

func TestDuplicateDocumentLinesHaveStableUniqueAIIDs(t *testing.T) {
	first, second := input(t), input(t)
	if first.Transactions[0].TransactionID == first.Transactions[1].TransactionID {
		t.Fatal("duplicate row IDs rejected by real AI service")
	}
	for i, tx := range first.Transactions {
		if tx.TransactionID != second.Transactions[i].TransactionID || len(tx.TransactionID) > 256 {
			t.Fatal("unstable or too long row ID")
		}
	}
}

func TestExplicitOrderAndStockMetadataReachAI(t *testing.T) {
	p := &domain.Product{Code1C: "001_", Supplier: "IEK", OrderMultiple: 5, Unit: "шт", MinimumOrderQuantity: ptr(0), QuantityStep: ptr(1), StockAsOfDate: "2026-09-22"}
	d := &domain.Dataset{AsOf: domain.DatasetDate(), Products: map[string]*domain.Product{p.Code1C: p}}
	r, err := Build(d, domain.RunConfig{Supplier: "IEK"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Products[0].Unit != "шт" || r.Products[0].MinimumOrderQuantity == nil || *r.Products[0].MinimumOrderQuantity != 0 || r.Inventory[0].StockAsOfDate != "2026-09-22" || r.Inventory[0].FreeStock != nil {
		t.Fatal("order metadata or unknown inventory changed")
	}
}

func TestCalendarHorizonAdaptsToAICoverageWithoutDoubleCountingLead(t *testing.T) {
	p := &domain.Product{Code1C: "001_", Supplier: "IEK"}
	d := &domain.Dataset{AsOf: domain.DatasetDate(), Products: map[string]*domain.Product{p.Code1C: p}}
	s, _ := Settings(domain.RunConfig{})
	for _, tc := range []struct{ months, lead, coverage int }{{1, 30, 30}, {2, 30, 61}, {3, 45, 91}} {
		s.ForecastHorizonMonths, s.LeadTimeDays = tc.months, tc.lead
		r, err := Build(d, domain.RunConfig{Supplier: "IEK", Settings: &s})
		if err != nil {
			t.Fatal(err)
		}
		if r.Settings.ReviewPeriodDays+r.Settings.LeadTimeDays != tc.coverage {
			t.Fatalf("wrong coverage: %+v", r.Settings)
		}
		encoded, err := json.Marshal(r)
		if err != nil || !strings.Contains(string(encoded), `"reviewPeriodDays":`) {
			t.Fatal("review interval must be explicit, including zero")
		}
	}
	s.ForecastHorizonMonths, s.LeadTimeDays = 1, 31
	if _, err := Build(d, domain.RunConfig{Supplier: "IEK", Settings: &s}); err == nil {
		t.Fatal("coverage shorter than lead time accepted")
	}
}
