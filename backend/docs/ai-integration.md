# Локальная интеграция Go и AI Service

Проверено 23.09.2026: существующий frontend → Go → настоящий Python AI-service →
Go → frontend. Изменения реализации ограничены `backend/`. Frontend и AI-service
не изменялись. Существующий импортёр Excel и маршруты frontend сохранены.

## Фактический контракт AI-разработчика

Источник: `ai-service/README.md`, `contracts/README.md`, `app/schemas.py`,
`app/settings.py`, `app/http_guard.py` и `app/algorithm.py`.

| Параметр | Значение |
|---|---|
| Базовый URL | `http://127.0.0.1:8001` |
| Расчёт | `POST /v1/recommendations`, JSON, `schemaVersion: "1.0"` |
| Health | `GET /health` |
| Авторизация | `Authorization: Bearer <AI_SERVICE_TOKEN>`, включая health |
| Deadline AI | `AI_REQUEST_TIMEOUT_SECONDS=30` |
| Timeout Go → AI | Deadline AI + 5 секунд, по умолчанию 35 секунд |
| Лимит партии AI по умолчанию | 500 товаров |
| Лимит JSON | AI 50 MiB; Go сохраняет свой более строгий лимит 50 MB |

Адрес, внутренний токен и deadline берутся из окружения Go. Ключ OpenAI Go не
использует и не передаёт. Внутренний токен не попадает в браузер или логи;
HTTP redirects запрещены. Поддерживаемый deadline в текущем Go: больше 0,
не больше 60 секунд, чтобы сетевой вызов укладывался в HTTP-контекст Go 75 секунд.
Frontend ожидает до 120 секунд. Для иного бюджета нужно согласовать все уровни.

Проверка выполнена в `AI_MODE=mock`, `ALLOW_EXTERNAL_AI=false`: используется
настоящий Python-расчёт; анализ аномалий демонстрационный, объяснение шаблонное.
OpenAI не вызывался. В ответе это явно указано через `MOCK_ANALYSIS`,
`RESULT_SOURCES`, `source=fallback` и статусы providers. Успех платных моделей
этой проверкой не подтверждается.

## Небольшие наборы

На этом компьютере создана папка `backend/data/demo/integration/`:

- `iek/`: шесть Excel с одним реальным SKU `010300004_` («УЗО АД 12 (2ф) 32А
  IEK (5/40)»), 68 исходных операций, 33 месяца продаж/остатков и исходная
  сезонность. Значения продаж, дат, документов и пропусков сверены с большим
  набором `data/demo/iek`; исходные книги не изменены.
- `system_electric/`: один **синтетический контрольный** товар `030200128_`
  из существующего генератора проекта. Явно добавлены тестовые значения:
  единица `шт`, минимальная партия 0, шаг 1, дата остатка 22.09.2026.
  Эти значения не относятся к реальным товарам.

Excel и результаты локальной проверки находятся в игнорируемом `data/demo`,
не публикуются в git. Для другого компьютера нужны соответствующие исходные
файлы; автоматической подстановки этих наборов нет.

## Команды запуска

Все команды ниже выполняются из корня репозитория. Если сервисы уже работают
на 8001/8080/5173, используйте их; повторный запуск на тех же портах не нужен.

Подготовка Python-окружения вне исходников AI-service:

```bash
python3 -m venv /tmp/supplylens-ai-integration-venv
/tmp/supplylens-ai-integration-venv/bin/python -m pip install -r ai-service/requirements.txt
```

Один раз создайте общий локальный внутренний токен. На проверенном компьютере
файл уже существует; команда не перезаписывает его и не печатает значение:

```bash
python3 - <<'PY'
import os, pathlib, secrets
directory = pathlib.Path('/tmp/supplylens-integration')
directory.mkdir(mode=0o700, exist_ok=True)
token = directory / 'service-token'
if not token.exists():
    with open(token, 'x', opener=lambda p, flags: os.open(p, flags, 0o600)) as output:
        output.write(secrets.token_urlsafe(32))
PY
```

Терминал 1 — AI:

```bash
export AI_SERVICE_TOKEN="$(cat /tmp/supplylens-integration/service-token)"
cd ai-service
PYTHONDONTWRITEBYTECODE=1 AI_MODE=mock ALLOW_EXTERNAL_AI=false AI_LOCAL_MODE=false \
  AI_REQUEST_TIMEOUT_SECONDS=30 \
  /tmp/supplylens-ai-integration-venv/bin/python -m uvicorn app.main:create_app \
  --factory --host 127.0.0.1 --port 8001 --no-proxy-headers
```

Терминал 2 — Go:

```bash
export AI_SERVICE_TOKEN="$(cat /tmp/supplylens-integration/service-token)"
cd backend
AI_SERVICE_URL=http://127.0.0.1:8001 AI_REQUEST_TIMEOUT_SECONDS=30 \
  DATA_DEMO_DIR=data/demo/integration \
  CORS_ORIGINS=http://127.0.0.1:5173,http://localhost:5173 \
  go run ./cmd/api
```

Терминал 3 — frontend:

```bash
cd frontend
# npm ci — если зависимости ещё не установлены
VITE_API_BASE_URL=http://127.0.0.1:8080 npm run dev -- --port 5173 --strictPort
```

Адрес интерфейса: **http://127.0.0.1:5173**. Выберите IEK → «Загрузить из
data/demo» → «Рассчитать заказ». Для положительного контрольного сценария
выберите SystemElectric; его товар явно назван демонстрационным.

## Подтверждённый общий запрос

Импорт через тот же маршрут, который использует frontend:

```bash
curl --fail-with-body http://127.0.0.1:8080/api/v1/imports/local \
  -H 'Content-Type: application/json' \
  -d '{"supplier":"IEK"}'
```

Подставьте полученный `importId` (в проверке: `import-20260923-002`):

```bash
curl --include http://127.0.0.1:8080/api/v1/imports/import-20260923-002/recommendations \
  -H 'Content-Type: application/json' \
  -d '{"forecastHorizonMonths":2,"leadTimeDays":30,"safetyStockDays":14,"excludePartialMonth":true}'
```

Подтверждённый результат реального IEK: **HTTP 422**, `AI_INCOMPLETE_DATA`.
В `error.details.result.recommendations[0]`:

```json
{
  "code1C": "010300004_",
  "processingStatus": "needs_review",
  "recommendedQuantity": null,
  "missingFields": [
    "inventory.stockAsOfDate",
    "inventory.freeStock",
    "incomingShipments"
  ],
  "calculation": {
    "coverageDays": 61,
    "forecastDemand": 36.4204716,
    "roundedRequirement": null
  }
}
```

Это сокращённая выдержка. Полный ответ, включая расчёт, объяснение, warnings,
статусы и null, сохранён в `data/demo/integration/response.json`; запрос —
`request.json`, импорт — `import.json`, снимок браузера — `real-browser.png`.
AI выполнил расчёт спроса, но реальных данных для количества заказа нет.
Числовая рекомендация для этого SKU пока **не подтверждена**.

Второй, синтетический сценарий прошёл с **HTTP 201**, `runId=run-001`,
`recommendedQuantity=240`, `coverageDays=61`, `status=COMPLETED_WITH_WARNINGS`.
Расчёт, объяснение и замечания показаны в неизменённом frontend. Результат —
`control-response.json`, снимки — `control-browser.png`, `control-explanation.png`.

Для повторения второго сценария используйте `supplier=SystemElectric` при
импорте и полученный `importId` в том же endpoint рекомендаций. ID хранятся
в памяти Go и меняются при новых импортах/перезапуске.

## Что адаптирует Go

- Транзакции не объединяются. При повторных или пустых номерах документов
  исходный номер остаётся в Dataset, AI получает стабильный уникальный ID строки.
- AI отличает `minimumOrderQuantity` от старого `moq` — кратности. Значения
  не выводятся друг из друга. Опциональные колонки MOQ: «Минимальная партия»,
  «Шаг количества», «Единица измерения»; единица также читается из «Ед.»
  месячных остатков. Пустые значения остаются неизвестными.
- Дата текущего остатка передаётся только из явной колонки «Дата остатка»
  в файле текущих остатков/поставок. Исторический остаток не становится текущим.
- Внутренний `reviewPeriodDays` = число дней выбранного календарного горизонта
  минус `leadTimeDays`. Поэтому Python получает нужное покрытие без повторного
  добавления срока поставки: для 22.09.2026 + 2 месяца это 61 день, review=31.
  Публичные поля frontend не менялись. Горизонт короче срока поставки отклоняется.
- Алгоритм AI сейчас использует только завершённые месяцы. Выбор
  `excludePartialMonth=false` возвращает 422 `AI_UNSUPPORTED_SETTINGS`;
  Go не выдаёт игнорирование настройки за успешный расчёт.
- Валидный ответ AI с неизвестным количеством возвращается как согласованный
  422 `AI_INCOMPLETE_DATA`. Его полный результат сохранён в `error.details.result`.
  Нулевой заказ остаётся числом 0. Ошибка не создаёт фиктивный пустой run и
  не удаляет импорт. Для отображения nullable-рекомендаций обычной таблицей
  потребуется отдельное согласование изменения frontend.
- Ошибка внутреннего токена → 502 `AI_AUTHENTICATION_FAILED`; недоступность AI
  или 503 → 503; deadline AI/Go → 504; валидация AI → 422; повреждённый результат
  → 502. Ошибки не заменяются успешными пустыми ответами. Расчётные поля,
  explanation и warnings успешного ответа передаются без пересчёта во frontend.

Никаких изменений БД, новых endpoints, облачного развёртывания или механизма
отправки заказов в рамках интеграции не добавлено.

## Проверки

```bash
go test -race ./backend/...
go vet ./backend/...
go build ./backend/...
```

Проверены Bearer-токен и запрет redirects, бюджет timeout, HTTP 401/503/504,
сохранение null/ошибок/объяснения, уникальные ID детальных строк, опциональные
колонки без придуманных значений, календарный горизонт и сохранение endpoint.
В браузере проверены оба набора через настоящий AI-service, CORS/preflight,
отсутствие внутренних токенов и прямых AI-вызовов из браузера, отображение
расчёта/объяснения и понятного 422. Необработанных JavaScript-ошибок нет.
