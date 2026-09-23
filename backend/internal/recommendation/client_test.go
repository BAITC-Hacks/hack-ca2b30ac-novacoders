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
