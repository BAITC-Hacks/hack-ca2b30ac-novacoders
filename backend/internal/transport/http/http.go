package httptransport

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/importer"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/recommendation"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/service"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/store"
)

const MaxUploadBytes int64 = 6*importer.MaxFileBytes + (1 << 20)

type API struct {
	service *service.Service
	logger  *slog.Logger
	origins map[string]bool
}

func New(s *service.Service, logger *slog.Logger, origins []string) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	a := &API{service: s, logger: logger, origins: map[string]bool{}}
	for _, o := range origins {
		if o = strings.TrimSpace(o); o != "" {
			a.origins[o] = true
		}
	}
	return http.HandlerFunc(a.serve)
}

func (a *API) serve(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	defer func() {
		if recover() != nil {
			a.logger.Error("http_panic")
			jsonError(w, 500, "INTERNAL_ERROR", "Внутренняя ошибка.", nil)
		}
		a.logger.Info("request_complete", "method", r.Method, "duration_ms", time.Since(started).Milliseconds())
	}()
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if origin := r.Header.Get("Origin"); origin != "" {
		w.Header().Add("Vary", "Origin")
		if !a.origins[origin] {
			jsonError(w, 403, "ORIGIN_NOT_ALLOWED", "Origin не разрешён.", nil)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")
	}
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	switch {
	case path == "healthz":
		if !method(w, r, "GET") {
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	case path == "api/v1/import":
		if !method(w, r, "POST") {
			return
		}
		a.importFiles(w, r)
	case path == "api/v1/runs":
		if !method(w, r, "POST") {
			return
		}
		var cfg domain.RunConfig
		if !decode(w, r, &cfg) {
			return
		}
		a.logger.Info("calculation_started")
		run, err := a.service.CreateRun(ctx, cfg)
		if err != nil {
			handleError(w, err)
			return
		}
		a.logger.Info("calculation_completed", "run_id", run.ID, "items", len(run.Items))
		writeJSON(w, 201, run)
	case len(parts) == 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "runs":
		if !method(w, r, "GET") {
			return
		}
		run, err := a.service.Store.Run(parts[3])
		if err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, 200, run)
	case len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "runs" && parts[4] == "items":
		if !method(w, r, "PATCH") {
			return
		}
		var patch domain.ManagerPatch
		if !decode(w, r, &patch) {
			return
		}
		run, err := a.service.Patch(ctx, parts[3], parts[5], patch)
		if err != nil {
			handleError(w, err)
			return
		}
		a.logger.Info("manager_update_completed", "run_id", run.ID)
		writeJSON(w, 200, run)
	case len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "runs" && parts[4] == "approve-export":
		if !method(w, r, "POST") {
			return
		}
		a.export(w, r, parts[3])
	case len(parts) == 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "datasets":
		if !method(w, r, "GET") {
			return
		}
		d, err := a.service.Store.Dataset(parts[3])
		if err != nil {
			handleError(w, err)
			return
		}
		writeJSON(w, 200, importResponse(d))
	default:
		jsonError(w, 404, "NOT_FOUND", "Маршрут не найден.", nil)
	}
}

func method(w http.ResponseWriter, r *http.Request, expected string) bool {
	if r.Method == expected {
		return true
	}
	w.Header().Set("Allow", expected)
	jsonError(w, 405, "METHOD_NOT_ALLOWED", "Метод не разрешён.", nil)
	return false
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		jsonError(w, 415, "UNSUPPORTED_MEDIA_TYPE", "Ожидается application/json.", nil)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		badJSON(w, err)
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		badJSON(w, err)
		return false
	}
	return true
}

func badJSON(w http.ResponseWriter, err error) {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		jsonError(w, 413, "PAYLOAD_TOO_LARGE", "JSON превышает 1 MiB.", nil)
		return
	}
	jsonError(w, 400, "INVALID_JSON", "Некорректный JSON, тип поля или неизвестное поле.", nil)
}

func (a *API) importFiles(w http.ResponseWriter, r *http.Request) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "multipart/form-data" {
		jsonError(w, 415, "UNSUPPORTED_MEDIA_TYPE", "Ожидается multipart/form-data.", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes)
	err = r.ParseMultipartForm(8 << 20)
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			jsonError(w, 413, "PAYLOAD_TOO_LARGE", "Превышен общий лимит загрузки.", nil)
		} else {
			jsonError(w, 400, "INVALID_MULTIPART", "Некорректная multipart-форма.", nil)
		}
		return
	}
	allowed := map[string]bool{}
	for _, field := range importer.Fields {
		allowed[field] = true
	}
	for name := range r.MultipartForm.File {
		if !allowed[name] {
			jsonError(w, 400, "UNKNOWN_FILE_FIELD", "Неизвестное поле файла.", nil)
			return
		}
	}
	for name, values := range r.MultipartForm.Value {
		if name != "supplier" || len(values) != 1 {
			jsonError(w, 400, "UNEXPECTED_FORM_FIELD", "Разрешены шесть файлов и одно поле supplier.", nil)
			return
		}
	}
	supplier := r.FormValue("supplier")
	if supplier == "" {
		supplier = domain.Supplier
	}
	if supplier != domain.Supplier && supplier != "IEK" {
		jsonError(w, 400, "INVALID_SUPPLIER", "supplier: SystemElectric или IEK", nil)
		return
	}
	files := map[string]io.Reader{}
	fileNames := map[string]string{}
	for _, field := range importer.Fields {
		parts := r.MultipartForm.File[field]
		if len(parts) != 1 {
			jsonError(w, 400, "MISSING_OR_DUPLICATE_FILE", "Требуется один файл в поле "+field+".", nil)
			return
		}
		if parts[0].Size > importer.MaxFileBytes {
			jsonError(w, 413, "PAYLOAD_TOO_LARGE", "Размер файла превышает 20 MiB.", nil)
			return
		}
		file, err := parts[0].Open()
		if err != nil {
			jsonError(w, 400, "INVALID_FILE", "Файл недоступен.", nil)
			return
		}
		defer file.Close()
		files[field] = file
		fileNames[field] = parts[0].Filename
	}
	a.logger.Info("import_started")
	d, err := importer.ImportSupplier(r.Context(), files, supplier)
	if err != nil {
		var invalid *importer.ValidationError
		if errors.As(err, &invalid) {
			jsonError(w, 422, "IMPORT_VALIDATION_FAILED", invalid.Error(), invalid.Diagnostics)
			return
		}
		handleError(w, err)
		return
	}
	d.SourceFiles = fileNames
	d, err = a.service.Store.AddDataset(d)
	if err != nil {
		handleError(w, err)
		return
	}
	a.logger.Info("import_completed", "dataset_id", d.ID, "products", len(d.Products))
	writeJSON(w, 201, importResponse(d))
}

func importResponse(d *domain.Dataset) any {
	return struct {
		DatasetID            string                   `json:"datasetId"`
		Products             int                      `json:"products"`
		FullyMatchedProducts int                      `json:"fullyMatchedProducts"`
		Warnings             []domain.Diagnostic      `json:"warnings"`
		Diagnostics          domain.ImportDiagnostics `json:"diagnostics"`
	}{d.ID, len(d.Products), d.Diagnostics.FullyMatchedProducts, d.Diagnostics.Warnings, d.Diagnostics}
}

func (a *API) export(w http.ResponseWriter, r *http.Request, id string) {
	run, err := a.service.Store.Run(id)
	if err != nil {
		handleError(w, err)
		return
	}
	var buffer bytes.Buffer
	exported := 0
	writer := csv.NewWriter(&buffer)
	_ = writer.Write([]string{"supplier", "code_1c", "article", "name", "quantity", "estimated_cost", "decision", "manager_comment"})
	for _, i := range run.Items {
		if i.ApprovedQuantity == nil || *i.ApprovedQuantity <= 0 {
			continue
		}
		exported++
		cost := ""
		if i.EstimatedCost != nil {
			cost = strconv.FormatFloat(*i.EstimatedCost, 'f', 2, 64)
		}
		_ = writer.Write([]string{csvText(i.Supplier), csvText(i.Code1C), csvText(i.Article), csvText(i.Name), strconv.FormatFloat(*i.ApprovedQuantity, 'f', 0, 64), cost, i.Decision, csvText(i.ManagerComment)})
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		handleError(w, err)
		return
	}
	if err := r.Context().Err(); err != nil {
		handleError(w, err)
		return
	}
	a.logger.Info("export_completed", "run_id", run.ID, "items", exported)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+run.ID+`.csv"`)
	w.WriteHeader(200)
	_, _ = w.Write(buffer.Bytes())
}

// Prevent imported text or a manager comment from becoming a spreadsheet formula.
func csvText(s string) string {
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(s, "\t") || strings.HasPrefix(s, "\r") || strings.HasPrefix(s, "\n") || (len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0]))) {
		return "'" + s
	}
	return s
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		jsonError(w, 500, "ENCODING_ERROR", "Не удалось сформировать ответ.", nil)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}

func jsonError(w http.ResponseWriter, status int, code, message string, details any) {
	writeJSON(w, status, struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details any    `json:"details,omitempty"`
		} `json:"error"`
	}{Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details any    `json:"details,omitempty"`
	}{code, message, details}})
}

func handleError(w http.ResponseWriter, err error) {
	var upstream *recommendation.Error
	if errors.As(err, &upstream) {
		jsonError(w, upstream.HTTPStatus, upstream.Code, upstream.Message, upstream)
		return
	}
	switch {
	case errors.Is(err, store.ErrNotFound):
		jsonError(w, 404, "NOT_FOUND", "Набор данных, расчёт или позиция не найдены.", nil)
	case errors.Is(err, service.ErrInvalid):
		jsonError(w, 400, "INVALID_INPUT", err.Error(), nil)
	case errors.Is(err, context.DeadlineExceeded):
		jsonError(w, 504, "TIMEOUT", "Время выполнения запроса истекло.", nil)
	case errors.Is(err, context.Canceled):
		jsonError(w, 408, "REQUEST_CANCELED", "Запрос отменён.", nil)
	default:
		jsonError(w, 500, "INTERNAL_ERROR", "Внутренняя ошибка.", nil)
	}
}
