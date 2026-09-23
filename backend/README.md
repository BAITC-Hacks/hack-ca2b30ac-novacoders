# SupplyLens backend

Go 1.26.1+, Excelize, datasets и расчёты в памяти. Рабочая цепочка:

```text
6 XLSX → Go: чтение, проверка, нормализация
       → POST AI_SERVICE_URL/v1/recommendations
       → AI Service: формулы, аномалии, объяснения
       → Go: сохранение ответа, решения менеджера, CSV
```

Go не вызывает OpenAI/NVIDIA напрямую. `cmd/api` всегда обращается к отдельному
AI Service при создании расчёта. При его недоступности возвращается ошибка,
импортированный dataset остаётся доступным для повторной попытки.

## Запуск и загрузка папки

Из корня репозитория:

```bash
cd backend
AI_SERVICE_URL=http://127.0.0.1:8001 go run ./cmd/api
```

AI Service нужно запустить отдельно на порту 8001. Его исходники и запуск этим
репозиторием не управляются. Проверки:

```bash
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8001/health
```

Во втором терминале, из `backend/`, одна команда загружает шесть XLSX, получает
`datasetId`, запускает расчёт и выводит результат JSON:

```bash
go run ./cmd/analyze -dir data/demo/iek -supplier IEK > /tmp/iek-result.json

go run ./cmd/analyze -dir data/demo/electric_system -supplier SystemElectric > /tmp/system-result.json
```

В текущем workspace папка называется **electric_system**. Если ваша папка
называется `system_electric`, укажите её фактический путь в `-dir`.
Каждый поставщик импортируется отдельным dataset. CLI распознаёт файлы по началам
названий: `MOQ`, `Динамика`, `Ежемесячные остатки`, `Ежемесячные продажи`,
`Сезонность`, `Товар в пути` / `Путь`. Пробелы в именах поддерживаются;
неоднозначный выбор файла вызывает ошибку.

Для проверки Excel без работающего AI Service:

```bash
go run ./cmd/analyze -dir data/demo/iek -supplier IEK -import-only > /tmp/iek-import.json
```

CLI принимает `-api http://127.0.0.1:8080`. `cmd/demo` **создаёт синтетические
файлы с четырьмя SKU**, а не анализирует существующие. Генерировать их можно в
отдельную папку: `go run ./cmd/demo -out data/generated`.
В командах Go обязательно `./cmd/api`, `./cmd/analyze`, `./cmd/demo` с `./`.

## Конфигурация

| Переменная | По умолчанию |
|---|---|
| `HTTP_ADDR` | `127.0.0.1:8080` |
| `AI_SERVICE_URL` | `http://127.0.0.1:8001` — базовый URL, без `/v1/recommendations` |
| `CORS_ORIGINS` | localhost/127.0.0.1 на портах 5173 и 3000, через запятую |

`.env` автоматически не читается. Авторизация AI Service в предоставленном
контракте отсутствует; backend не отправляет API-ключи провайдеров.
`/healthz` проверяет только Go-процесс. SIGINT/SIGTERM выполняет graceful shutdown.
**После перезапуска теряются datasets, расчёты и подтверждения менеджера.**
Пользовательской авторизации в этом MVP нет.

## Endpoints Go Backend

| Метод и путь | Принимает | Возвращает |
|---|---|---|
| `GET /healthz` | — | 200 `{"status":"ok"}` |
| `POST /api/v1/import` | multipart: 6 файлов, `supplier` | 201: `datasetId`, количество товаров, диагностика |
| `GET /api/v1/datasets/{datasetId}` | — | 200: та же сводка импорта |
| `POST /api/v1/runs` | JSON: `datasetId`, `supplier`, необязательные `settings` | 201: сохранённый расчёт с ответом AI |
| `GET /api/v1/runs/{runId}` | — | 200: расчёт и текущие подтверждения |
| `PATCH /api/v1/runs/{runId}/items/{code1C}` | JSON: подтверждение количества, комментарий, решение об аномалиях | 200: обновлённый расчёт |
| `POST /api/v1/runs/{runId}/approve-export` | — | 200: CSV только явно подтверждённых количеств > 0 |

### Импорт

Поля multipart: `moq`, `sales_transactions`, `monthly_stock`, `monthly_sales`,
`seasonality`, `in_transit`; ровно один XLSX в каждом. `supplier` — `IEK` или
`SystemElectric` (по умолчанию). Можно загружать из frontend обычным `FormData`.

Пример для реальных имён IEK, из `backend/`:

```bash
curl --fail-with-body -sS http://127.0.0.1:8080/api/v1/import \
  -F 'supplier=IEK' \
  -F 'moq=@data/demo/iek/MOQ  ИЭК.xlsx' \
  -F 'sales_transactions=@data/demo/iek/Динамика продаж_2025-2026.xlsx' \
  -F 'monthly_stock=@data/demo/iek/Ежемесячные остатки продукции за последние 2 года  ИЭК.xlsx' \
  -F 'monthly_sales=@data/demo/iek/Ежемесячные продажи в количественном выражении за последние 2 года.xlsx' \
  -F 'seasonality=@data/demo/iek/Сезонность ИЭК.xlsx' \
  -F 'in_transit=@data/demo/iek/Путь ИЭК 22.09.2026.xlsx'
```

Ответ содержит `diagnostics.processedRows`, `skippedTotalRows`,
`productsWithoutMOQ`, `productsWithoutSales`, `productsWithoutCurrentStock`,
`sourceConflicts`, `warnings`, `errors`. Ошибки отдельных ячеек не отменяют чтение
всей книги; они остаются в диагностике и требуют проверки. Некорректная книга,
отсутствие файла/обязательных заголовков — 422 без сохранения dataset.

Коды сопоставляются точно как строки: ведущие нули и `_` сохраняются. Числовые
значения читаются без Excel-форматирования, поэтому разделители тысяч не меняют
количество. Отрицательные продажи сохраняются. Дубли SKU в месячных данных
суммируются по `(code1C, month)`. Если хотя бы одна часть суммы неизвестна или
невалидна, результат — `null`, не частичная сумма. Дубли детальных операций
не объединяются; `Номер` документа передаётся как `transactionId`, при пустом
номере используется `Документ`. Дубли MOQ/текущих остатков требуют проверки:
сохраняется первая строка с блокирующей диагностикой.

Для IEK поддержаны многострочные месячные заголовки, MOQ из `Мин. разр. к отгр.`,
сезонность из первой таблицы `Месяц` + `СЕЗОННОСТЬ`, партии из колонок
`поступление до ...`. Пустая партия означает **неизвестное количество**:
если есть хотя бы одна такая ячейка, `inTransit: null`; сумма известных партий
указывается только в диагностике. В данном наборе IEK нет текущего свободного
остатка и себестоимости: `freeStock`, `totalStock`, `reservedStock`, `unitCost`
передаются как `null`. Остаток на начало месяца не подставляется вместо текущего.

### Запуск расчёта

Минимальный запрос использует настройки v1 по умолчанию:

```bash
curl --fail-with-body -sS http://127.0.0.1:8080/api/v1/runs \
  -H 'Content-Type: application/json' \
  -d '{"datasetId":"dataset-001","supplier":"IEK"}'
```

Используйте ID из ответа импорта. Полный запрос с настройками:

```json
{
  "datasetId": "dataset-001",
  "supplier": "IEK",
  "settings": {
    "historyMonths": 12,
    "forecastHorizonMonths": 2,
    "leadTimeDays": 30,
    "safetyStockDays": 14,
    "excludePartialMonth": true,
    "availableStockPolicy": "FREE_PLUS_IN_TRANSIT",
    "anomalyReviewEnabled": true,
    "explanationMode": "IMPORTANT_ONLY",
    "maxAIExplanations": 50
  }
}
```

Если передаёте `settings`, задавайте объект целиком. Поддержанные ограничения:
history 1–120 месяцев, horizon 1–36 месяцев, lead time 1–365 дней, safety 0–365,
max explanations 0–1000. В v1 поддержаны указанные выше policy и explanationMode.
Для прежних клиентов разрешены верхнеуровневые `leadTimeDays`/`safetyDays`
вместо `settings`; их нельзя смешивать. Дополнительные склады (`includeShowcase`,
`includeTZStock`, `includeRetailStock`) нельзя включить в этом контракте.

Backend сам формирует `schemaVersion: "1.0"`, UUID `requestId`,
`asOfDate: "2026-09-22"` (дата среза этих файлов), `currency: "KZT"`,
`sourceMeta` с именами файлов и числом прочитанных строк, а также массивы
`products`, `monthlySales`, `monthlyStock`, `transactions`, `inventory`,
`seasonality`. Даты операций — `YYYY-MM-DD`, месяцы — `YYYY-MM`.
Отсутствующие значения передаются `null`, явные нули — `0`; отсутствующие месяцы
не добавляются искусственно. Excel в AI Service не отправляется.

Ответ Go:

```json
{
  "runId": "run-001",
  "datasetId": "dataset-001",
  "createdAt": "2026-09-23T10:30:00Z",
  "config": {},
  "summary": {"buy":0,"noBuy":0,"review":1,"totalUnits":25,"estimatedCost":31250,"unpricedItems":0,"approvedItems":0},
  "items": [],
  "aiResponse": {
    "schemaVersion":"1.0",
    "requestId":"UUID запроса",
    "runId":"ID AI Service",
    "status":"COMPLETED_WITH_WARNINGS",
    "summary": {},
    "providers": {},
    "recommendations": []
  }
}
```

Это сокращённая схема; настоящий ответ содержит все позиции. Верхний `runId`
принадлежит Go; `aiResponse.runId` — внешнему сервису. `aiResponse` сохраняется
полностью и не меняется после решений менеджера, включая объяснения, статусы
провайдеров, расчётные поля и предупреждения.

`items` содержит товары для работы менеджера: `decision` (из AI `action`),
`urgency`, `recommendedQuantity`, `approvedQuantity`, `finalQuantity`,
`unitCost`, `estimatedCost`, `calculation`, `explanation`, `anomalyAnalysis`,
`anomalies`, `warnings`, `requiresManualReview`, `confidence`, `managerComment`.
Расчётные поля `calculation` передаются без пересчёта и сохраняют `null`.
`finalQuantity` равен подтверждённому количеству, иначе предварительной рекомендации.
Сводка Go суммирует текущие `finalQuantity`; `aiResponse.summary` — исходная сводка AI.

Go проверяет версию, requestId, успешный статус, полный набор уникальных SKU,
корректность действий и количества. Несовпадение `recommendedQuantity` и
`calculation.roundedRequirement`, нарушение MOQ, неизвестные остатки и конфликты
источников переводят локальную позицию в `REVIEW`, сохраняя исходные цифры AI.
Статусы провайдеров `FALLBACK`/`FAILED` внутри успешно выполненного расчёта допустимы.
Отказ всего AI Service не заменяется незаметно локальным расчётом.

### Подтверждения и CSV

```bash
curl --fail-with-body -sS -X PATCH \
  http://127.0.0.1:8080/api/v1/runs/run-001/items/030200128_ \
  -H 'Content-Type: application/json' \
  -d '{"approvedQuantity":100,"comment":"Проверено менеджером"}'

curl --fail-with-body -sS -X POST \
  http://127.0.0.1:8080/api/v1/runs/run-001/approve-export \
  -o /tmp/approved.csv
```

`approvedQuantity` — целое 0…1e12; 0 означает отказ от заказа. Подтверждение
`REVIEW` должно быть явным. Экспорт сам не подтверждает позиции и ничего
не отправляет поставщику. Изменение комментария сохраняет одобрение.

`anomalyDecision: "EXCLUDE" | "KEEP"` сохраняется отдельно для всех кандидатов
позиции и при изменении сбрасывает прежнее подтверждение. **Контракт AI v1 не
принимает решения менеджера для повторного расчёта**: backend не пересчитывает
исходную рекомендацию после этого PATCH. Возвращается
`ANOMALY_DECISION_RECORDED`; итоговое количество нужно подтвердить явно.
Для автоматического пересчёта с подтверждёнными исключениями потребуется
расширить входной контракт AI. NVIDIA самостоятельно подтверждение не создаёт.

CSV: `supplier,code_1c,article,name,quantity,estimated_cost,decision,manager_comment`.
Неизвестная стоимость — пустая ячейка. Текстовые формулы экранируются;
при открытии в Excel импортируйте `code_1c` как текст.

## Ошибки, лимиты, проверки

Ошибки: `{"error":{"code":"...","message":"...","details":{}}}`.
400 — входной запрос; 404 — ресурс; 413 — размер; 415 — Content-Type;
422 — импорт/валидация AI; 502 — отказ/невалидный ответ AI;
503 `AI_SERVICE_UNAVAILABLE` — сервис недоступен; 504 — таймаут.
AI-валидация сохраняет `requestId` и ошибки `fields` в `error.details`.

20 MiB на XLSX, 121 MiB на multipart, 128 MiB распакованной книги,
250 000 строк на лист, 1 MiB на входной JSON Go, 50 MB на запрос/ответ AI.
HTTP AI timeout 50 секунд, контекст запроса Go — 60 секунд.
Логи содержат этапы и счётчики без исходных таблиц и комментариев.

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

Пакеты `internal/forecast` и `internal/anomaly` с проверенными локальными
формулами сохранены; `cmd/api` не использует их для построения рекомендаций.
Сверка источников и суммирование результатов остаются задачами backend.
Тесты покрывают импорт обоих форматов, разделители тысяч, нули/null/возвраты,
дубли, HTTP-контракт с тестовым AI Service, ошибки/таймауты, согласованность
ответа, отдельные подтверждения и экспорт. Проверка с настоящим AI Service
требует запущенного сервиса на `AI_SERVICE_URL`.
