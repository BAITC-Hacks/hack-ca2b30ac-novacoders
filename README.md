# SupplyLens

Автоматизация подготовки заказа: **React → Go Backend → отдельный AI Service**.
Frontend загружает Excel, показывает проверку и рекомендации; менеджер явно
подтверждает количество, после чего скачивает CSV. Заказ поставщику не отправляется.
Данные и подтверждения хранятся в памяти Go и теряются при перезапуске.

## Локальный запуск

1. Запустите AI Service из его собственного репозитория с endpoint
   `POST /v1/recommendations` на порту 8001. Исходников этого сервиса здесь нет;
   точная команда запуска определяется его проектом. Контракт: [alibekAI.md](alibekAI.md).
   Проверка доступности: `curl http://127.0.0.1:8001/health`.
2. В отдельном терминале запустите backend:

   ```bash
   cd backend
   AI_SERVICE_URL=http://127.0.0.1:8001 go run ./cmd/api
   ```

3. В другом терминале запустите frontend:

   ```bash
   cd frontend
   npm ci
   cp .env.example .env
   npm run dev
   ```

   Откройте http://127.0.0.1:5173. Если `.env` уже существует, обновите только
   `VITE_API_BASE_URL=http://127.0.0.1:8080`, сохранив остальные настройки.

Выберите поставщика SystemElectric/IEK и нажмите **«Загрузить из data/demo»**.
Go прочитает шесть Excel из `backend/data/demo/iek` или
`backend/data/demo/system_electric`. Поддерживается прежнее имя `electric_system`;
если существуют обе папки SystemElectric, импорт потребует оставить один набор.
Файлы в корне `data/demo` не подставляются вместо файлов поставщика.
Каждый поставщик получает отдельный импорт и расчёт со своей сезонностью.
Вместо серверного набора можно вручную выбрать шесть `.xlsx`. Дождитесь проверки,
задайте горизонт/срок поставки/страховой запас, затем запустите рекомендации.
Откройте товар → «Решение менеджера» → «Подтвердить количество» → скачайте CSV.
`REVIEW` требует такого же явного подтверждения. Черновик, снятое подтверждение
и подтверждённый ноль не включаются в экспорт.

Frontend никогда не обращается к AI Service напрямую и не содержит ключей
провайдеров. Отсутствие AI Service возвращается как ошибка; фиктивных результатов
в рабочем сценарии нет. Fallback-результат, сформированный самим AI Service,
отображается с предупреждениями.

## API

| Метод | Endpoint | Назначение |
|---|---|---|
| POST | `/api/v1/imports` | Шесть XLSX → importId и диагностика |
| POST | `/api/v1/imports/local` | `{ "supplier": "IEK" }` или SystemElectric → импорт файлов из data/demo |
| GET | `/api/v1/imports/{importId}` | Сохранённая проверка файлов |
| POST | `/api/v1/imports/{importId}/recommendations` | Параметры → вызов AI Service и сохранённый расчёт |
| GET | `/api/v1/recommendations/{runId}` | Результат и актуальные подтверждения |
| PATCH | `/api/v1/recommendations/{runId}/items/{code1C}` | Решение менеджера |
| POST | `/api/v1/recommendations/{runId}/export` | CSV подтверждённых позиций |
| GET | `/healthz` | Доступность Go-процесса |

Описание контрактов, ограничений и CLI: [backend/README.md](backend/README.md).
Результат проверки текущих Excel: [backend/docs/data-audit.md](backend/docs/data-audit.md).
Устройство клиента и интерфейса: [frontend/README.md](frontend/README.md).

## Проверки

```bash
cd backend
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

```bash
cd frontend
npm run lint
npm run typecheck
npm run test
npm run build
```

Backend integration test выполняет XLSX → importId → HTTP AI Service → BUY/NO_BUY/REVIEW
→ подтверждение → CSV. Тестовый AI HTTP-сервер существует только внутри теста.
Frontend-тесты проверяют точные поля multipart/JSON, null и нули, fallback,
ошибки и освобождение Blob URL. Настоящий AI Service нужен для рабочего расчёта.
