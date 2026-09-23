# SupplyLens frontend

React + TypeScript + Vite. Единственный рабочий сценарий использует Go Backend.
Демо-API, искусственные задержки, автоматические тестовые рекомендации и график
с синтетической историей удалены. Backend-генератор Excel сохранён отдельно для
тестирования загрузки; он не подставляется в интерфейс автоматически.

## Запуск

```bash
npm ci
cp .env.example .env
npm run dev
```

Если `.env` уже существует, сохраните его и настройте только нужные переменные.
Страница: http://127.0.0.1:5173.

```env
VITE_API_BASE_URL=http://127.0.0.1:8080
```

Это адрес **Go Backend**. Не помещайте ключи OpenAI/NVIDIA или URL AI Service в
`VITE_*`: браузер не обращается к провайдерам. При явно пустом `VITE_API_BASE_URL`
используется same-origin `/api`; в dev Vite проксирует его на `API_PROXY_TARGET`
(по умолчанию http://127.0.0.1:8080). CORS backend разрешает localhost/127.0.0.1:5173.

## Контракт и экраны

API разделён по ответственности:

- `src/api/client.ts`: HTTP/JSON/multipart, таймауты и вложенные ошибки backend.
- `src/api/imports.ts`: загрузка серверного набора, шесть multipart-полей и проверка результата импорта.
- `src/api/recommendations.ts`: создание/чтение расчёта, PATCH, Blob/CSV и cleanup URL.
- `src/api/types.ts`: единые camelCase-типы wire-контракта.

React-компоненты не содержат URL и не вычисляют количество заказа.

1. Импорт принимает `.xlsx` до 20 MiB на файл и поставщика SystemElectric/IEK.
   Кнопка «Загрузить из data/demo» вызывает `POST /api/v1/imports/local` с
   `{supplier}`: Go читает папку выбранного поставщика на сервере. Это реальные
   файлы на диске, без подстановки frontend-fixture. Имя `electric_system`
   поддерживается наряду с `system_electric`. Каждый поставщик анализируется отдельно.
   Поля: `moqFile`, `monthlySalesFile`, `detailedSalesFile`, `monthlyStockFile`,
   `seasonalityFile`, `inventoryTransitFile`; текстовое поле `supplier`.
2. После `POST /api/v1/imports` показываются `importId`, проверка каждого файла,
   счётчики товаров и замечания. Большой список замечаний раскрывается частями.
3. `POST /api/v1/imports/{importId}/recommendations` принимает только
   `forecastHorizonMonths`, `leadTimeDays`, `safetyStockDays`, `excludePartialMonth`.
4. Таблица показывает решения BUY/NO_BUY/REVIEW, фильтры, данные расчёта,
   объяснения и аномалии. `null` отображается как «Нет данных», `0` как ноль.
5. `PATCH /api/v1/recommendations/{runId}/items/{code1C}` отправляет
   `approvedQuantity`, явный `approved`, `comment`, при необходимости
   `anomalyDecision`. После PATCH клиент читает актуальный run через GET,
   чтобы сводки оставались серверными. Если обновление не удалось, экспорт
   блокируется до повторного чтения.
6. `POST /api/v1/recommendations/{runId}/export` возвращает CSV; Object URL
   освобождается после запуска скачивания. Итог заказа берётся из `orderSummary`.

«Сохранить без подтверждения» сохраняет черновик. «Подтвердить количество»
подтверждает в том числе REVIEW; ноль означает отказ от заказа. Отмена
подтверждения исключает позицию из экспорта, не меняя исходную рекомендацию.
Изменение решения об аномалии сбрасывает подтверждение. Контракт AI v1 пока
не предусматривает пересчёт по решению менеджера; это указано в карточке.

Loading, ошибки сервера и пустые состояния показаны отдельно. Во время запроса
повторная отправка блокируется. Переход на ещё недоступный этап не генерирует
данные. Новый успешный расчёт начинает отдельный набор подтверждений.

## Проверки

Существующие команды проекта:

```bash
npm run lint
npm run typecheck
npm run test
npm run build
```

Тесты API используют заглушки только внутри `.test.ts`. Они проверяют точные
маршруты, multipart-поля, явное подтверждение, сохранность `code1C`, `null`/`0`,
ошибки AI/backend и скачивание CSV. Полный серверный сценарий дополнительно
проверяется Go integration test в `backend/internal/transport/http/contracts_test.go`.
