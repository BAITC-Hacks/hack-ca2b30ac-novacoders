# SupplyLens AI Service

FastAPI-сервис рассчитывает прогноз и количество закупки Python-кодом через существующий `POST /v1/recommendations`. Синтетический пример даёт **90**, увеличение своевременной поставки с 30 до 70 даёт **50**. В mock анализ демонстрационный, объяснение — Python-шаблон. В live подключена последовательность OpenAI analysis → проверка → разрешённые бизнес-корректировки/Python-расчёт → OpenAI explanation. Точные числа всегда формирует Python.

## Установка и запуск

Нужен исправный Python 3.11+; проверка этого этапа выполняется на Python 3.13. Управление зависимостями — `venv` + `pip` + requirements; ранее в сервисе менеджера зависимостей не было. Lock-файлы не создаются и не меняются.

```sh
cd /Users/alibek/hack-ca2b30ac-novacoders/ai-service
python3.13 -m venv .venv313
.venv313/bin/python -m pip install -r requirements-dev.txt
AI_MODE=mock ALLOW_EXTERNAL_AI=false AI_LOCAL_MODE=true AI_SERVICE_TOKEN= \
  .venv313/bin/python -m uvicorn app.main:create_app --factory \
  --host 127.0.0.1 --port 8001 --no-proxy-headers
```

На данном компьютере Python 3.13 расположен по `/usr/local/bin/python3.13`; если команда не находится через PATH, используйте полный путь. Python 3.14 из Homebrew при создании окружения завершился ошибкой `pyexpat`; системные библиотеки не изменялись. Созданное при этой попытке `.venv/` не используется.

Сервис читает **только окружение процесса**, не загружает `.env` автоматически. `.env.example` — справочник настроек с пустыми секретами; копировать его поверх существующего `.env` не нужно. Приведённая команда принудительно включает локальный mock без внешних вызовов и без токена. Для нелокального доступа обязателен внутренний токен, передаваемый через окружение; ключи не помещать в исходники или команды, сохраняемые в истории.

В другом терминале:

```sh
cd /Users/alibek/hack-ca2b30ac-novacoders/ai-service
curl --fail-with-body http://127.0.0.1:8001/health
curl --fail-with-body http://127.0.0.1:8001/v1/recommendations \
  -H 'Content-Type: application/json' \
  --data-binary @contracts/request.example.json
```

При заданном `AI_SERVICE_TOKEN` оба маршрута требуют Bearer-токен. CORS не включён. Локальный обход авторизации предназначен для прямого подключения к loopback; не публикуйте его через reverse proxy.

## Передача Go-разработчику

Адрес при локальном запуске: `http://127.0.0.1:8001`. Расчёт: **POST /v1/recommendations**; проверка доступности: **GET /health**. Примеры: `contracts/request.example.json`, `contracts/response.example.json`; оба проверяются моделями HTTP API. Схема и правила полей: [contracts/README.md](contracts/README.md).

Для подключения Go заполните в окружении процесса `AI_SERVICE_TOKEN` одинаковым значением на обеих сторонах. В mock ключ OpenAI не нужен. Для live: `AI_MODE=live`, `ALLOW_EXTERNAL_AI=true`, непустой `OPENAI_API_KEY`, `AI_SERVICE_TOKEN`; `OPENAI_MODEL` — существующая выбранная модель, пустые `OPENAI_ANALYSIS_MODEL` / `OPENAI_EXPLANATION_MODEL` наследуют её. Ключи читаются только из окружения процесса, `.env` автоматически не загружается. Ключ OpenAI не передаётся в JSON или HTTP-заголовке от Go.

Запуск с настройками из подготовленного окружения (токен не переопределяется):

```sh
.venv313/bin/python -m uvicorn app.main:create_app --factory \
  --host 127.0.0.1 --port 8001 --no-proxy-headers
```

Заголовок внутренней авторизации: `Authorization: Bearer <AI_SERVICE_TOKEN>`. Он нужен и для health. Пример запроса из окружения, где задан токен:

```sh
curl --fail-with-body --max-time 35 \
  http://127.0.0.1:8001/v1/recommendations \
  -H "Authorization: Bearer ${AI_SERVICE_TOKEN}" \
  -H 'Content-Type: application/json' \
  --data-binary @contracts/request.example.json
```

Установите HTTP timeout Go **35 секунд** при `AI_REQUEST_TIMEOUT_SECONDS=30`: deadline клиента должен быть больше deadline AI-сервиса с запасом на передачу ответа (например, +5 секунд). При изменении deadline сервиса измените Go timeout. Товары в live обрабатываются последовательно; большая партия может исчерпать общий бюджет, после чего AI-этапы получают явный fallback. Go не должен автоматически повторять live POST: идемпотентного кеша нет, повторы могут повторно оплатить вызовы.

| Значение | Как читать |
| --- | --- |
| `AI_MODE=mock` | Без сети: mock analysis, настоящий Python-расчёт, template explanation. Поэтому общий source сейчас fallback, а не mock |
| `source=mock` | Зарезервированное значение схемы для демонстрационного результата; фактический mock analysis обозначен отдельно в anomalyAnalysis.source и is_demo |
| `AI_MODE=live` / `source=live` | Разрешены OpenAI-вызовы / AI-этапы данного ответа выполнены без зарегистрированных сбоев; расчёт всё равно Python |
| `source=fallback` | Ответ собран с deterministic/template; причину и точные источники смотрите в warnings/RESULT_SOURCES каждой позиции |
| `processingStatus=needs_review` | Нужна проверка. При критическом пробеле recommendedQuantity=null и missingFields; при предупреждениях число может быть предварительно рассчитано |

HTTP 200 не гарантирует успешность AI или готовность позиции к закупке: проверьте `processingStatus`, `requiresManualReview`, `anomalyAnalysis.status`, `warnings`, `source`. Не заменяйте null нулём. При сбое объяснения количество остаётся прежним. Поля providers.nvidia и nvidiaPrompt — только сохранённые placeholders совместимости, второго провайдера нет.

Ошибки имеют вид `{"requestId":null,"error":{"code":"...","message":"...","fields":[]}}`; requestId может быть известен. HTTP: 400 — невалидный JSON; 401 — авторизация; 413 — размер; 415 — Content-Type; 422 — схема/лимиты; 503 AI_NOT_CONFIGURED — live без ключа; 504 — deadline; 500 — внутренняя ошибка. Сбои OpenAI после начала расчёта обычно возвращают HTTP 200 с явно отмеченным fallback/FAILED и шаблоном.

## Настройки

| Переменная | Default | Назначение |
| --- | --- | --- |
| `AI_MODE` | `mock` | `mock` — offline; `live` — OpenAI analysis/explanation с Python-расчётом |
| `ALLOW_EXTERNAL_AI` | `false` | Запрет внешних AI-запросов; live требует true |
| `AI_LOCAL_MODE` | `false` | Только явно включённый локальный mock допускает отсутствие токена |
| `AI_SERVICE_TOKEN` | пусто | Внутренняя авторизация |
| `OPENAI_API_KEY` | пусто | Один ключ для анализа и объяснения; mock не требует ключей |
| `OPENAI_MODEL` | `gpt-6-luna` | Базовая модель обеих задач |
| `OPENAI_ANALYSIS_MODEL` | пусто | Пустое значение → OPENAI_MODEL |
| `OPENAI_EXPLANATION_MODEL` | пусто | Пустое значение → OPENAI_MODEL |
| `AI_REQUEST_TIMEOUT_SECONDS` | `30` | Общий таймаут запроса |
| `AI_PROVIDER_TIMEOUT_SECONDS` | `15` | Общий deadline вызова: очередь, запрос и повторы |
| `AI_PROVIDER_MAX_RETRIES` | `1` | Повторы только внутри SDK, от 0 до 3 |
| `AI_MAX_OUTPUT_TOKENS` | `2048` | Лимит выходных токенов |
| `AI_ANALYSIS_MAX_ROWS` | `120` | Максимальное число строк одной серии в контексте |
| `AI_ANALYSIS_MAX_CONTEXT_BYTES` | `65536` | Лимит сериализованного контекста |
| `AI_MAX_REQUEST_BYTES` | `52428800` | Размер JSON, включая потоковую передачу |
| `AI_MAX_PRODUCTS` | `500` | Число товаров |
| `AI_MAX_CONCURRENT_API_CALLS` | `2` | Общий лимит адаптеров на экземпляр в одном процессе |

Пустые ключи не заменяются выдуманными. Настройки не логируются. Health-check не обращается к провайдерам и не проверяет доступ к модели; livePipelineAvailable отражает только наличие live-конфигурации и непустого ключа.

## Структура

- `app/main.py`, `app/routes.py`, `app/http_guard.py` — сборка HTTP-приложения, маршруты, авторизация, размер и таймаут.
- `app/schemas.py` — модели входа и выхода, alias, связи и валидация.
- `app/settings.py` — окружение и безопасные режимы запуска.
- `app/providers/clients.py` — общий AsyncOpenAI для задач analysis/explanation, создаётся и закрывается в lifespan приложения. В mock SDK-клиент не создаётся.
- `app/analysis/` — сборка контекста одной серии, OpenAI/mock-провайдеры, проверка предложений и результат анализа.
- `app/prompts/analysis_v1.txt` — версия промпта анализа.
- `app/compat.py` — сохранение устаревших внешних полей без вызова прежнего провайдера.
- `app/algorithm.py` — календарный baseline, проверяемые корректировки, расчёт закупки.
- `app/pipeline.py` — последовательный анализ, Python-расчёт и объяснение; fallback при сбоях.
- `app/explanation/`, `app/prompts/explanation_v1.txt` — короткое качественное объяснение по готовым компонентам.
- `contracts/` — примеры и описание совместимости с `alibekAI.md`.
- `tests/` — обычные тесты без интернета и ключей; сетевые подключения запрещены fixture.

Официальный SDK используется согласно [OpenAI SDK documentation](https://developers.openai.com/api/docs/libraries); указанное пользователем имя [gpt-6-luna](https://developers.openai.com/api/docs/models/gpt-6-luna) сохранено. Используются Responses API и Pydantic Structured Outputs. Реальных вызовов не выполнялось; доступ аккаунта к модели не проверялся.

## Проверки

```sh
.venv313/bin/python -m pytest -q
.venv313/bin/python -m compileall -q app tests
.venv313/bin/python -m pip check
git check-ignore -v .env
git ls-files -- .env '.env.*'
git diff --check -- .
```

Тесты проверяют HTTP API, JSON-примеры, snake_case/camelCase, сохранение строковых артикулов/null, дубли и неоднозначные ссылки, раздельные склад/поставщик, неизменность входа, авторизацию, лимиты, таймауты и отсутствие реальных вызовов. SDK проверяется локальной подменой HTTP-транспорта: реальные сериализация запроса и Pydantic-парсинг выполняются без сети. Анализ дополнительно проверяется mock-провайдером. Установка зависимостей требует доступа к реестру пакетов; выполнение обычных тестов — нет.

Ограничения и расширения контракта: [contracts/README.md](contracts/README.md). Текущий этап: [docs/pipeline.md](docs/pipeline.md). Расчётный baseline: [docs/mvp.md](docs/mvp.md). Этап 3: [docs/openai-analysis.md](docs/openai-analysis.md). Исторический этап 2: [docs/implementation.md](docs/implementation.md).

## Внутренний сервис анализа

`build_context(request, product, settings)` строит контекст одного составного ключа.
`await app.state.analysis_service.analyze_series(context)` возвращает `AnalysisResult`.
Сервис создаётся в lifespan; в mock он использует `MockAnalysisProvider`.
HTTP pipeline использует lifecycle AnalysisService: MockAnalysisProvider в mock и OpenAIAnalysisProvider в live. ExplanationService использует тот же lifecycle ProviderClients.

`status=failed` с error_code означает ошибку анализа, а не отсутствие аномалий.
Успешный ответ без предложений требует явного `no_anomalies_reason`.
Статус каждого предложения назначает Python: `needs_review`; предложения не применяются. Независимо от них алгоритм обрабатывает подтверждённые бизнес-метки.

Только `https://api.openai.com/v1`; внешние URL не настраиваются через JSON или окружение.
Пустые переопределения модели используют базовую модель. Ошибка доступа не переключает модель.
Передаются `store=false` и лимит токенов, tools не подключены.
`store=false` не является обещанием полного отсутствия хранения у провайдера:
см. [OpenAI data controls](https://developers.openai.com/api/docs/guides/your-data).
В прикладных логах — только тип задачи, фиксированный код ошибки, источник и число предложений;
нет ключей, контекста, текста ответов или SDK-исключений. Не включайте отладочное логирование тел HTTP-запросов.


## Smoke pipeline

Без сети и ключей:

```sh
.venv313/bin/python scripts/smoke_pipeline.py
.venv313/bin/python -m pytest tests/test_live_pipeline.py -q
```

Первое — ручной mock smoke с синтетическим JSON; второе — полный live pipeline с подменённым HTTP-транспортом, настоящими SDK/Pydantic-парсерами и запретом сети.

Для ручной проверки **реального OpenAI** только после отдельного разрешения: экспортируйте OPENAI_API_KEY и AI_SERVICE_TOKEN безопасным способом в окружение, а затем выполните:

```sh
ALLOW_EXTERNAL_AI=true .venv313/bin/python scripts/smoke_pipeline.py --live
```

Этот вариант требует явного --live, делает внешние запросы и может расходовать средства; автоматически он не запускался. Скрипт проверяет один синтетический товар и принудительно устанавливает максимум один анализ, одно объяснение и ai_provider_max_retries=0, независимо от настройки повторов окружения. Настройки моделей берутся из существующего окружения. .env не загружается автоматически. Ошибка анализа помечается FAILED/needs_review; ошибка объяснения сохраняет template и число заказа. Код выхода smoke: 0 — успех, 1 — ошибка конфигурации/HTTP/выполнения, 2 — live вернул fallback. Ошибки конфигурации и провайдера выводятся без секретов и полного тела ответа.


Последняя offline-передача: **152 теста прошли**; ручной mock smoke вернул 90. Проверены арифметика 90/50, монотонность заказа, MOQ/кратность, неизменность истории, null-остаток, неподтверждённый stockout, неизвестный row_id, сбои анализа/объяснения, схемы примеров, HTTP/авторизация и отсутствие сети. Live smoke не запускался. Есть одно предупреждение Starlette/httpx о deprecated интеграции TestClient; тесты проходят.
