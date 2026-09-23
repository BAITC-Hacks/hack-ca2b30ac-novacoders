package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/demo"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
)

func TestLocalSupplierDirectorySelection(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"iek", "electric_system"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for supplier, name := range map[string]string{"IEK": "iek", domain.Supplier: "electric_system"} {
		if dir, err := LocalDirectory(root, supplier); err != nil || filepath.Base(dir) != name {
			t.Fatalf("%s %v", dir, err)
		}
	}
	if _, err := LocalDirectory(root, "../iek"); err == nil {
		t.Fatal("arbitrary paths accepted")
	}
	if err := os.Rename(filepath.Join(root, "electric_system"), filepath.Join(root, "system_electric")); err != nil {
		t.Fatal(err)
	}
	if dir, err := LocalDirectory(root, domain.Supplier); err != nil || filepath.Base(dir) != "system_electric" {
		t.Fatalf("%s %v", dir, err)
	}
	if err := os.Mkdir(filepath.Join(root, "electric_system"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := LocalDirectory(root, domain.Supplier); err == nil {
		t.Fatal("ambiguous datasets accepted")
	}
}

func TestDirectoryImportsActualFilesAndRejectsAmbiguity(t *testing.T) {
	dir := t.TempDir()
	workbooks, err := demo.Workbooks()
	if err != nil {
		t.Fatal(err)
	}
	for field, data := range workbooks {
		if err := os.WriteFile(filepath.Join(dir, demo.Filenames[field]), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	lock := filepath.Join(dir, "~$MOQ SystemElectric.xlsx")
	if err := os.WriteFile(lock, []byte("Excel lock"), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := ImportDirectory(context.Background(), dir, domain.Supplier)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Products) != 4 || d.SourceFiles["moq"] != demo.Filenames["moq"] {
		t.Fatal("source metadata missing")
	}
	duplicate := filepath.Join(dir, "MOQ duplicate.xlsx")
	if err := os.WriteFile(duplicate, workbooks["moq"], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportDirectory(context.Background(), dir, domain.Supplier); err == nil {
		t.Fatal("duplicate source accepted")
	}
	if err := os.Remove(duplicate); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, demo.Filenames["monthly_stock"])); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportDirectory(context.Background(), dir, domain.Supplier); err == nil {
		t.Fatal("missing file replaced with demo data")
	}
}
