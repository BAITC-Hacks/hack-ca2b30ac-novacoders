# SupplyLens Go Backend

Go 1.26.1+, Excelize, thread-safe in-memory store с mutex. Backend читает и
проверяет Excel, нормализует данные, отправляет их в отдельный AI Service,
сохраняет ответ и отдельные решения менеджера. БД и Docker не используются.

## Запуск

```bash
cd backend
AI_SERVICE_URL=http://127.0.0.1:8001 go run ./cmd/api
```

Из корня репозитория также работает:

```bash
AI_SERVICE_URL=http://127.0.0.1:8001 go run ./backend/cmd/api
```

Корневой `go.work` подключает модуль `backend` для Go и редактора. Если редактор
показывает `a.localDataRoot undefined`, а `go test ./backend/...` проходит,
проверьте несохранённую старую версию `backend/internal/transport/http/http.go`
и выполните **Go: Restart Language Server** в палитре команд VS Code.

| Переменная | Значение |
|---|---|
| `AI_SERVICE_URL` | Обязательный базовый URL из environment, без `/v1/recommendations` |
| `HTTP_ADDR` | По умолчанию `127.0.0.1:8080` |
| `DATA_DEMO_DIR` | По умолчанию папка `data/demo` внутри backend; явный относительный путь отсчитывается от рабочей директории |
| `CORS_ORIGINS` | По умолчанию localhost/127.0.0.1 на портах 5173 и 3000 |

Пример: `.env.example`. Go не загружает `.env` автоматически. Backend не содержит
ключей и клиентов OpenAI/NVIDIA. `/healthz` проверяет только Go-процесс.
AI Service запускается отдельно; исходников и команды его запуска в этом
репозитории нет. Проверка: `curl http://127.0.0.1:8001/health`.
Перезапуск Go удаляет импорты, расчёты и подтверждения. Пользовательская
авторизация в MVP не реализована. SIGINT/SIGTERM завершает процесс корректно.

## Контракт с frontend

### Импорт

`POST /api/v1/imports`, multipart/form-data:

| Поле | Источник |
|---|---|
| `moqFile` | MOQ/кратность |
| `monthlySalesFile` | Месячные продажи |
| `detailedSalesFile` | Детальные операции |
| `monthlyStockFile` | Исторические остатки |
| `seasonalityFile` | Сезонность |
| `inventoryTransitFile` | Текущие остатки / партии в пути |

По одному `.xlsx` в каждом поле. Необязательный `supplier`: `SystemElectric`
(по умолчанию) или `IEK`. Неизвестные поля отвергаются.

Ответ 201:

```json
{
  "importId": "import-20260923-001",
  "status": "READY",
  "supplier": "SystemElectric",
  "asOf": "2026-09-22",
  "files": [{"type":"MOQ","fileName":"MOQ SystemElectric.xlsx","status":"VALID","rows":554}],
  "summary": {"productsFound":554,"productsReady":467,"productsWithWarnings":87},
  "warnings": []
}
```

В настоящем ответе `files` содержит шесть записей. Возможные статусы файла:
`VALID`/`WARNING`. `READY` означает, что dataset сохранён и доступен для анализа;
это не отсутствие замечаний. `productsReady` — товары без найденных замечаний
с нужными источниками, остальные учитываются в `productsWithWarnings`.
`GET /api/v1/imports/{importId}` возвращает ту же проверку.
Ошибки отдельных ячеек остаются в диагностике; некорректная книга/заголовки
дают 422 без сохранения импорта.

Код 1С — точная строка, включая ведущие нули и `_`. Количества читаются из
исходных значений Excel, без влияния отображаемых разделителей тысяч.
Отрицательные возвраты сохраняются. Месячные дубли суммируются по `(code1C, month)`;
если часть суммы отсутствует/невалидна, итог — `null`. Детальные строки не
объединяются; отсутствующее количество операции — `null`. Дубли MOQ/текущих
остатков требуют проверки: первая строка сохраняется с предупреждением.

IEK поддерживает многострочные заголовки, `Мин. разр. к отгр.`, таблицу
`Месяц` + `СЕЗОННОСТЬ` и отдельные партии с `поступление до ...`.
Пустая партия — неизвестное количество: если хотя бы одна партия пуста,
общий `inTransit` неизвестен; сумма известных партий указана в предупреждении.
В текущих IEK-файлах нет свободного/общего/зарезервированного текущего остатка
и цены: в AI уходит `null`. Исторический остаток на начало месяца не заменяет
текущий. Дата среза этого набора фиксирована: **22.09.2026**.

### Рекомендации

`POST /api/v1/imports/{importId}/recommendations`:

```json
{
  "forecastHorizonMonths": 2,
  "leadTimeDays": 30,
  "safetyStockDays": 14,
  "excludePartialMonth": true
}
```

Все четыре поля обязательны. Horizon 1–36 месяцев, lead time 1–365 дней,
safety 0–365. Ноль и явный false сохраняются.

Backend формирует контракт AI v1 с UUID `requestId`, `schemaVersion: "1.0"`,
датой среза, KZT, метаданными файлов и массивами `products`, `monthlySales`,
`monthlyStock`, `transactions`, `inventory`, `seasonality`. Дополнительные
настройки: historyMonths 12, availableStockPolicy FREE_PLUS_IN_TRANSIT,
anomalyReviewEnabled true, explanationMode IMPORTANT_ONLY, maxAIExplanations 50.
Excel в AI Service не отправляется. Запрос: `POST {AI_SERVICE_URL}/v1/recommendations`.
Клиент использует context, timeout **60 секунд**, лимит 50 MB и проверяет HTTP
status, JSON, версию, requestId, полный набор SKU, действия и количества.

Ответ 201 и `GET /api/v1/recommendations/{runId}` имеют одинаковый формат:

- `runId`, `importId`, `supplier`, `asOf`, `createdAt`, `settings`;
- `status`: COMPLETED / COMPLETED_WITH_WARNINGS;
- `items`: code1C, article, name, category, supplier, decision, urgency,
  recommendedQuantity, approvedQuantity (nullable), approved, finalQuantity,
  updatedAt, estimatedCost (nullable), unitCost (nullable), calculation,
  explanation, anomalyAnalysis, anomalyDecision, warnings, managerComment;
- `summary`: buy, noBuy, review, totalUnits, estimatedCost, unpricedItems, approvedItems;
- `orderSummary`: positions, totalUnits, estimatedCost (null, если часть цен неизвестна);
- `warnings` и `providers`: предупреждения и статусы внешнего анализа.

`summary` описывает текущий предварительный результат, а `orderSummary` —
только подтверждённые количества > 0. `decision` не меняется на BUY от нажатия
«Подтвердить»: оценка данных и решение менеджера хранятся отдельно.
`calculation` и объяснения передаются без пересчёта, включая null.
Несогласованные quantity/roundedRequirement, MOQ, стоимость и неизвестные
остатки помечаются REVIEW. Исходный ответ AI хранится неизменным.
Успешный fallback от AI с недоступным OpenAI/NVIDIA показывается с предупреждениями.
При недоступности всего AI Service фиктивный run не создаётся; импорт сохраняется.

### Решение менеджера

`PATCH /api/v1/recommendations/{runId}/items/{code1C}`:

```json
{
  "approvedQuantity": 25,
  "approved": true,
  "anomalyDecision": "EXCLUDED",
  "comment": "Разовая продажа подтверждена"
}
```

Поля необязательны, но хотя бы одно обязательно. `approved: true` требует
явного `approvedQuantity` в том же запросе. Количество — целое 0…1e12.
Количество без `approved: true` сохраняется как черновик. `approved: false`
снимает подтверждение. Подтверждённый ноль — отказ от заказа.
Изменение комментария сохраняет предыдущее одобрение.

`anomalyDecision`: EXCLUDED / INCLUDED / PENDING_REVIEW, для всех кандидатов
одной позиции. Изменение решения сбрасывает предыдущее одобрение, если новое
не передано в том же PATCH. **AI v1 не принимает решения менеджера для повторного
расчёта**: исходная рекомендация не пересчитывается. Возвращается предупреждение
ANOMALY_DECISION_RECORDED; конечное количество подтверждается явно.

Ответ 200: `code1C`, `approvedQuantity`, `approved`, `updatedAt`.
Frontend затем GET-читает актуальный run, включая серверные сводки.
Все обновления store атомарны; сетевые вызовы выполняются вне mutex.

### Экспорт

`POST /api/v1/recommendations/{runId}/export`:

```http
Content-Type: text/csv; charset=utf-8
Content-Disposition: attachment; filename="supplier-order.csv"
```

Колонки: supplier, code_1c, article, name, quantity, estimated_cost, decision,
manager_comment. Только `approved: true` и количество > 0, включая REVIEW,
явно одобренный менеджером. Пустой заказ содержит только заголовок.
Неизвестная цена — пустая ячейка. Текстовые формулы экранируются.
Экспорт ничего не подтверждает и не отправляет заказ поставщику.

## CLI и совместимость

### Наборы в data/demo

При запуске из `backend/` сервер читает `data/demo/iek` и
`data/demo/system_electric` (также поддерживается прежнее имя `electric_system`).
Другой корень задаётся через `DATA_DEMO_DIR`. Поставщики импортируются отдельно;
файлы непосредственно в корне `data/demo` не используются как запасной набор.
Исходные книги не изменяются и не генерируются автоматически.

```bash
curl -X POST http://127.0.0.1:8080/api/v1/imports/local \
  -H 'Content-Type: application/json' \
  -d '{"supplier":"IEK"}'
```

Для второго набора передайте `"supplier":"SystemElectric"`. Ответ — тот же
контракт `importId/status/files/summary/warnings`, что у multipart-импорта.
Далее вызовите `/api/v1/imports/{importId}/recommendations` с параметрами расчёта.
Эта же последовательность доступна в UI через «Загрузить из data/demo».
Endpoint принимает только поставщика; произвольный путь передать нельзя.
Отсутствующий файл, неоднозначное имя папки или два файла одного типа дают 422.

В проверенном локальном наборе SystemElectric сейчас **4 синтетических товара**,
в IEK — **3185 товаров**. Для реального расчёта SystemElectric замените все шесть
файлов соответствующими отчётами поставщика. Подробности: [аудит данных](docs/data-audit.md).

### Загрузка папки через CLI

Для существующего `cmd/analyze` сохранены отдельные legacy-маршруты:
`POST /api/v1/import`, `GET /api/v1/datasets/{id}`, `POST /api/v1/runs`,
`GET /api/v1/runs/{id}`, `PATCH /api/v1/runs/{id}/items/{code}`,
`POST /api/v1/runs/{id}/approve-export`. Их прежний формат не смешивается с
новым контрактом. Только legacy PATCH подразумевает одобрение при передаче
approvedQuantity без approved. Frontend использует исключительно новые маршруты.

```bash
# Из backend/, после запуска Go и AI Service:
go run ./cmd/analyze -dir data/demo/iek -supplier IEK > /tmp/iek-result.json
go run ./cmd/analyze -dir data/demo/electric_system -supplier SystemElectric > /tmp/system-result.json
# Только импорт, без AI:
go run ./cmd/analyze -dir data/demo/iek -supplier IEK -import-only
```

Укажите фактическое имя папки, если оно другое. `cmd/demo` — отдельный генератор
синтетических XLSX: `go run ./cmd/demo -out data/generated`. Эти данные нужны
тестам и не подставляются в production-flow. Пакеты локальных формул остаются
покрыты тестами, но `cmd/api` получает рекомендации через AI Service.

Для папок `iek`, `system_electric`, `electric_system` параметр `-supplier` можно
не указывать: он определяется по имени папки. Для произвольной папки задайте его явно.

Сверка сохраняет все замечания в импорте. При расчёте блокирующие замечания по
месяцам учитываются только внутри `historyMonths` с учётом `excludePartialMonth`.
Неизвестные MOQ/текущие остатки требуют проверки независимо от периода.
Пропущенный месяц продаж отличается от месяца с продажами 0; нехватка истории
вызывает `INCOMPLETE_SALES_HISTORY`. Неизвестное количество операции сохраняется
как `null`, включая некорректную числовую ячейку с диагностикой. `SOURCE_CONFLICT`
содержит обе суммы, а `SOURCE_COMPARISON_UNAVAILABLE` означает невозможность сверки.
Полная исходная история передаётся в AI; прогнозные формулы остаются в AI Service.

## Ошибки и проверки

Ошибка: `{"error":{"code":"...","message":"...","details":{}}}`.
400 — запрос, 404 — ресурс, 413 — размер, 415 — Content-Type,
422 — данные/валидация AI, 502 — некорректный ответ/отказ AI, 503 — недоступность,
504 — таймаут. Валидация AI сохраняет requestId/fields в details; внутренние
ошибки и ключи не раскрываются.

20 MiB на файл, 121 MiB на multipart, 128 MiB распакованной книги, 250 000 строк
на лист, 1 MiB JSON frontend, 50 MB JSON AI. Контекст Go-запроса 75 секунд.

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
# Необязательная проверка реальных локальных IEK-файлов:
SUPPLYLENS_IEK_DIR="$PWD/data/demo/iek" go test ./internal/recommendation -run TestRealIEKContract -v
# Оба набора из папок поставщиков, без вызова AI:
SUPPLYLENS_DATA_DIR="$PWD/data/demo" go test ./internal/recommendation -run TestLocalDatasetsContract -v
```

`contracts_test.go` проверяет полный новый HTTP-flow, BUY/NO_BUY/REVIEW,
реальные XLSX fixture, fallback, черновики, явный ноль, отмену/сброс подтверждения,
CSV, CORS, конкурентные обращения и недоступность AI без подстановки результата.
