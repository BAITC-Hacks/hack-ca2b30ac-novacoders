package httptransport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/demo"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/service"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/store"
)

func TestLocalImportUsesSelectedDirectoryAndReturnsCanonicalImport(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "system_electric")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	files, err := demo.Workbooks()
	if err != nil {
		t.Fatal(err)
	}
	for field, data := range files {
		if err := os.WriteFile(filepath.Join(dir, demo.Filenames[field]), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	svc := &service.Service{Store: store.New()}
	h := New(svc, nil, nil, WithLocalDatasets(root))
	w := request(h, "POST", "/api/v1/imports/local", `{"supplier":"SystemElectric"}`)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var imported importDTO
	if err := json.Unmarshal(w.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.ImportID == "" || imported.Supplier != "SystemElectric" || imported.Summary.ProductsFound != 4 || len(imported.Files) != 6 {
		t.Fatalf("%+v", imported)
	}
	if request(h, "GET", "/api/v1/imports/"+imported.ImportID, "").Code != 200 {
		t.Fatal("local import not stored")
	}
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"supplier":"IEK"}`, 422}, // Never substitute the other supplier's files.
		{`{"supplier":"../../"}`, 400},
		{`{"supplier":"SystemElectric","dir":"/tmp"}`, 400},
		{`{}`, 400},
	} {
		if w := request(h, "POST", "/api/v1/imports/local", test.body); w.Code != test.status {
			t.Fatalf("%s: %d %s", test.body, w.Code, w.Body.String())
		}
	}
	disabled := New(svc, nil, nil)
	if w := request(disabled, "POST", "/api/v1/imports/local", `{"supplier":"SystemElectric"}`); w.Code != 503 {
		t.Fatalf("%d", w.Code)
	}
	if err := os.Remove(filepath.Join(dir, demo.Filenames["moq"])); err != nil {
		t.Fatal(err)
	}
	if w := request(h, "POST", "/api/v1/imports/local", `{"supplier":"SystemElectric"}`); w.Code != 422 {
		t.Fatal("missing workbook must fail")
	}
}
