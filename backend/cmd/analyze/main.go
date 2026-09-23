// analyze uploads a directory containing six source workbooks, then asks the
// Go backend to call the separate recommendation service.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/importer"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	dir := flag.String("dir", "", "Папка с шестью XLSX")
	supplier := flag.String("supplier", domain.Supplier, "SystemElectric или IEK")
	api := flag.String("api", "http://127.0.0.1:8080", "URL Go Backend")
	importOnly := flag.Bool("import-only", false, "Только импорт; без вызова AI")
	flag.Parse()
	if *dir == "" || (*supplier != domain.Supplier && *supplier != "IEK") {
		return fmt.Errorf("пример: go run ./cmd/analyze -dir data/demo/iek -supplier IEK")
	}
	entries, err := os.ReadDir(*dir)
	if err != nil {
		return err
	}
	files := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || strings.ToLower(filepath.Ext(e.Name())) != ".xlsx" || strings.HasPrefix(e.Name(), "~$") {
			continue
		}
		name := strings.ToLower(e.Name())
		field := ""
		switch {
		case strings.HasPrefix(name, "moq"):
			field = "moq"
		case strings.HasPrefix(name, "динамика"):
			field = "sales_transactions"
		case strings.HasPrefix(name, "ежемесячные остатки"):
			field = "monthly_stock"
		case strings.HasPrefix(name, "ежемесячные продажи"):
			field = "monthly_sales"
		case strings.HasPrefix(name, "сезонность"):
			field = "seasonality"
		case strings.HasPrefix(name, "товар в пути"), strings.HasPrefix(name, "путь"):
			field = "in_transit"
		}
		if field == "" {
			continue
		}
		if files[field] != "" {
			return fmt.Errorf("неоднозначный выбор файла для %s", field)
		}
		files[field] = filepath.Join(*dir, e.Name())
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("supplier", *supplier); err != nil {
		return err
	}
	for _, field := range importer.Fields {
		path := files[field]
		if path == "" {
			return fmt.Errorf("в папке не найден файл для %s", field)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		part, err := writer.CreateFormFile(field, filepath.Base(path))
		if err != nil {
			f.Close()
			return err
		}
		n, err := io.Copy(part, io.LimitReader(f, importer.MaxFileBytes+1))
		f.Close()
		if err != nil {
			return err
		}
		if n > importer.MaxFileBytes {
			return fmt.Errorf("файл %s превышает 20 MiB", path)
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	base := strings.TrimRight(*api, "/")
	client := &http.Client{Timeout: 90 * time.Second}
	imported, err := post(client, base+"/api/v1/import", writer.FormDataContentType(), body.Bytes())
	if err != nil {
		return err
	}
	var info struct {
		DatasetID string `json:"datasetId"`
		Products  int    `json:"products"`
	}
	if err := json.Unmarshal(imported, &info); err != nil {
		return err
	}
	if info.DatasetID == "" {
		return fmt.Errorf("backend не вернул datasetId")
	}
	fmt.Fprintf(os.Stderr, "Импортирован %s: %d товаров (%s).\n", info.DatasetID, info.Products, *supplier)
	if *importOnly {
		_, err = os.Stdout.Write(append(imported, '\n'))
		return err
	}
	data, err := json.Marshal(domain.RunConfig{DatasetID: info.DatasetID, Supplier: *supplier})
	if err != nil {
		return err
	}
	result, err := post(client, base+"/api/v1/runs", "application/json", data)
	if err != nil {
		return fmt.Errorf("%w\nDataset сохранён: %s. Повторить расчёт можно POST /api/v1/runs с этим datasetId.", err, info.DatasetID)
	}
	_, err = os.Stdout.Write(append(result, '\n'))
	return err
}
func post(client *http.Client, url, contentType string, body []byte) ([]byte, error) {
	res, err := client.Post(url, contentType, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 50_000_001))
	if err != nil {
		return nil, err
	}
	if len(data) > 50_000_000 {
		return nil, fmt.Errorf("ответ превышает 50 MB")
	}
	if res.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("backend HTTP %d: %s", res.StatusCode, string(data))
	}
	return data, nil
}
