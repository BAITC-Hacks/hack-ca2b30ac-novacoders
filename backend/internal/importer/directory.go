package importer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BAITC-Hacks/hack-ca2b30ac-novacoders/internal/domain"
)

// LocalDirectory selects one supplier only. Never mix supplier seasonality or
// join unrelated products across directories, even when their codes coincide.
func LocalDirectory(root, supplier string) (string, error) {
	var names []string
	switch supplier {
	case "IEK":
		names = []string{"iek"}
	case domain.Supplier:
		names = []string{"system_electric", "electric_system"}
	default:
		return "", fmt.Errorf("supplier: SystemElectric или IEK")
	}
	selected := ""
	for _, name := range names {
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("папка %s недоступна", name)
		}
		if selected != "" {
			return "", fmt.Errorf("найдены обе папки system_electric и electric_system: оставьте один набор, чтобы исключить неоднозначность")
		}
		selected = path
	}
	if selected == "" {
		return "", fmt.Errorf("нет папки %s в каталоге данных", strings.Join(names, " или "))
	}
	return selected, nil
}

// DiscoverDirectory requires exactly one workbook for each source; temporary
// Excel lock files are ignored. The files themselves are never modified.
func DiscoverDirectory(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать папку данных")
	}
	files := map[string]string{}
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if entry.IsDir() || filepath.Ext(name) != ".xlsx" || strings.HasPrefix(name, "~$") {
			continue
		}
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
			return nil, fmt.Errorf("найдено несколько файлов для %s", field)
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > MaxFileBytes {
			return nil, fmt.Errorf("%s: нужен обычный непустой файл до 20 MiB", entry.Name())
		}
		files[field] = filepath.Join(dir, entry.Name())
	}
	for _, field := range Fields {
		if files[field] == "" {
			return nil, fmt.Errorf("в папке не найден файл для %s", field)
		}
	}
	return files, nil
}

func ImportDirectory(ctx context.Context, dir, supplier string) (*domain.Dataset, error) {
	paths, err := DiscoverDirectory(dir)
	if err != nil {
		return nil, err
	}
	readers, names := map[string]io.Reader{}, map[string]string{}
	for _, field := range Fields {
		file, err := os.Open(paths[field])
		if err != nil {
			return nil, fmt.Errorf("не удалось открыть %s", filepath.Base(paths[field]))
		}
		defer file.Close()
		readers[field], names[field] = file, filepath.Base(paths[field])
	}
	d, err := ImportSupplier(ctx, readers, supplier)
	if err != nil {
		return nil, err
	}
	d.SourceFiles = names
	return d, nil
}
