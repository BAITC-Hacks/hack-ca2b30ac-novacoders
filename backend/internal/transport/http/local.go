package httptransport

import (
	"errors"
	"net/http"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/importer"
)

func (a *API) importLocal(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Supplier string `json:"supplier"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Supplier != "IEK" && input.Supplier != domain.Supplier {
		jsonError(w, 400, "INVALID_SUPPLIER", "supplier: SystemElectric или IEK", nil)
		return
	}
	if a.localDataRoot == "" {
		jsonError(w, 503, "LOCAL_DATA_UNAVAILABLE", "Серверная папка данных не настроена. Загрузите шесть Excel-файлов.", nil)
		return
	}
	dir, err := importer.LocalDirectory(a.localDataRoot, input.Supplier)
	if err != nil {
		jsonError(w, 422, "LOCAL_DATA_UNAVAILABLE", err.Error(), nil)
		return
	}
	d, err := importer.ImportDirectory(r.Context(), dir, input.Supplier)
	if err != nil {
		if r.Context().Err() != nil {
			handleError(w, r.Context().Err())
			return
		}
		var invalid *importer.ValidationError
		if errors.As(err, &invalid) {
			jsonError(w, 422, "IMPORT_VALIDATION_FAILED", invalid.Error(), invalid.Diagnostics)
		} else {
			jsonError(w, 422, "LOCAL_DATA_UNAVAILABLE", err.Error(), nil)
		}
		return
	}
	d, err = a.service.Store.AddDataset(d)
	if err != nil {
		handleError(w, err)
		return
	}
	a.logger.Info("local_import_completed", "supplier", input.Supplier, "dataset_id", d.ID, "products", len(d.Products))
	writeJSON(w, http.StatusCreated, importResult(d))
}
