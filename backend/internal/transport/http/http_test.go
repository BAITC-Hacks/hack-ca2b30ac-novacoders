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
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/importer"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/service"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/store"
)

func request(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func importDemo(t *testing.T, h http.Handler) string {
	t.Helper()
	files, err := demo.Workbooks()
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for field, data := range files {
		part, err := form.CreateFormFile(field, demo.Filenames[field])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/import", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var result struct {
		DatasetID string `json:"datasetId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.DatasetID
}

func handler() http.Handler {
	return New(&service.Service{Store: store.New()}, slog.New(slog.NewTextHandler(io.Discard, nil)), []string{"http://localhost:5173"})
}

func TestFullImportRunApproveExportFlow(t *testing.T) {
	h := handler()
	id := importDemo(t, h)
	w := request(h, "POST", "/api/v1/runs", `{"datasetId":"`+id+`","supplier":"SystemElectric","leadTimeDays":30,"safetyDays":14}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var run domain.CalculationRun
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.Summary.Buy != 1 || run.Summary.NoBuy != 1 || run.Summary.Review != 2 {
		t.Fatalf("%+v", run.Summary)
	}
	var response struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, item := range response.Items {
		if item["breakdown"] == nil || item["anomalies"] == nil || item["warnings"] == nil || item["explanation"] != nil {
			t.Fatal("response must expose calculation facts without generated explanations")
		}
	}
	w = request(h, "POST", "/api/v1/runs/"+run.ID+"/approve-export", "")
	rows, err := csv.NewReader(w.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatal("unapproved items leaked")
	}
	w = request(h, "PATCH", "/api/v1/runs/"+run.ID+"/items/030200128_", `{"approvedQuantity":100,"comment":"Подтверждено"}`)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.Items[0].FinalQuantity != 100 || run.Summary.ApprovedItems != 1 || *run.Items[0].EstimatedCost != 100000 {
		t.Fatalf("%+v", run)
	}
	w = request(h, "POST", "/api/v1/runs/"+run.ID+"/approve-export", "")
	rows, err = csv.NewReader(w.Body).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1][1] != "030200128_" || rows[1][4] != "100" || rows[1][7] != "Подтверждено" {
		t.Fatalf("%v", rows)
	}
	// Explicitly approving zero removes the item from the next export.
	w = request(h, "PATCH", "/api/v1/runs/"+run.ID+"/items/030200128_", `{"approvedQuantity":0}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request(h, "POST", "/api/v1/runs/"+run.ID+"/approve-export", "")
	rows, err = csv.NewReader(w.Body).ReadAll()
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %v", rows, err)
	}
}

func TestJSONErrorsCORSAndValidation(t *testing.T) {
	h := handler()
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{{"GET", "/unknown", "", 404}, {"GET", "/api/v1/import", "", 405}, {"POST", "/api/v1/runs", "{}", 400}, {"POST", "/api/v1/runs", `{"unexpected":1}`, 400}, {"POST", "/api/v1/runs", `{} {}`, 400}, {"POST", "/api/v1/runs", `{"datasetId":"x","supplier":"SystemElectric","leadTimeDays":30}`, 404}} {
		w := request(h, tc.method, tc.path, tc.body)
		if w.Code != tc.status || !json.Valid(w.Body.Bytes()) || !strings.Contains(w.Body.String(), `"error"`) {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct {
		origin string
		status int
	}{{"http://localhost:5173", 204}, {"https://untrusted.example", 403}} {
		r := httptest.NewRequest("OPTIONS", "/api/v1/runs", nil)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(w.Code)
		}
	}
}

func TestConcurrentRunReadsAndPatches(t *testing.T) {
	h := handler()
	id := importDemo(t, h)
	w := request(h, "POST", "/api/v1/runs", `{"datasetId":"`+id+`","supplier":"SystemElectric","leadTimeDays":30}`)
	var run domain.CalculationRun
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			var w *httptest.ResponseRecorder
			if i%2 == 0 {
				w = request(h, "PATCH", "/api/v1/runs/"+run.ID+"/items/030200128_", `{"approvedQuantity":50}`)
			} else {
				w = request(h, "GET", "/api/v1/runs/"+run.ID, "")
			}
			if w.Code != 200 {
				t.Errorf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	wg.Wait()
}

func TestCSVFormulaEscaping(t *testing.T) {
	if csvText(" =HYPERLINK(x)") != "' =HYPERLINK(x)" || csvText("030200128_") != "030200128_" {
		t.Fatal("unsafe CSV")
	}
}

func TestJSONAndFileSizeLimits(t *testing.T) {
	h := handler()
	w := request(h, "PATCH", "/api/v1/runs/run-001/items/sku", `{"comment":"`+strings.Repeat("x", 1<<20)+`"}`)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("JSON limit: %d %s", w.Code, w.Body.String())
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("moq", "moq.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, strings.NewReader(strings.Repeat("x", int(importer.MaxFileBytes)+1))); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/import", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("file limit: %d %s", w.Code, w.Body.String())
	}
}
