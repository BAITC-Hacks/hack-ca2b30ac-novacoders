Ниже готовый контракт v1 между Go Backend и отдельным AI Service.

## 1. Общая схема

```text
Excel → Go Backend → POST /v1/recommendations → AI Service
                                              ├─ расчёты
                                              ├─ NVIDIA: аномалии
                                              └─ OpenAI: объяснения
```

```env
AI_SERVICE_URL=http://127.0.0.1:8001
```

Полный URL:

```text
POST http://127.0.0.1:8001/v1/recommendations
```

Go передаёт не Excel-файлы, а нормализованный JSON.

---

# 2. Запрос Backend → AI Service

```http
POST /v1/recommendations
Content-Type: application/json
```

```json
{
  "schemaVersion": "1.0",
  "requestId": "93a40813-12dd-4aef-829d-4803251f6814",
  "asOfDate": "2026-09-22",
  "currency": "KZT",

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
  },

  "sourceMeta": {
    "moq": {
      "fileName": "MOQ SystemElectric.xlsx",
      "rowCount": 554
    },
    "monthlySales": {
      "fileName": "Ежемесячные продажи SystemElectric.xlsx",
      "rowCount": 554
    },
    "detailedSales": {
      "fileName": "Динамика продаж SystemElectric.xlsx",
      "rowCount": 77313
    },
    "monthlyStock": {
      "fileName": "Ежемесячные остатки SystemElectric.xlsx",
      "rowCount": 701
    },
    "seasonality": {
      "fileName": "Сезонность SystemElectric.xlsx",
      "rowCount": 12
    },
    "inventoryTransit": {
      "fileName": "Товар в пути SystemElectric.xlsx",
      "rowCount": 497
    }
  },

  "products": [
    {
      "code1C": "300200317_",
      "article": "SE-317",
      "name": "Автоматический выключатель",
      "supplier": "SystemElectric",
      "category": "1",
      "moq": 5,
      "unitCost": 1250
    }
  ],

  "monthlySales": [
    {
      "code1C": "300200317_",
      "month": "2026-06",
      "quantity": 21
    },
    {
      "code1C": "300200317_",
      "month": "2026-07",
      "quantity": 24
    },
    {
      "code1C": "300200317_",
      "month": "2026-08",
      "quantity": 19
    },
    {
      "code1C": "300200317_",
      "month": "2026-09",
      "quantity": 421
    }
  ],

  "monthlyStock": [
    {
      "code1C": "300200317_",
      "month": "2026-06",
      "quantity": 42
    },
    {
      "code1C": "300200317_",
      "month": "2026-07",
      "quantity": 20
    },
    {
      "code1C": "300200317_",
      "month": "2026-08",
      "quantity": 0
    },
    {
      "code1C": "300200317_",
      "month": "2026-09",
      "quantity": 15
    }
  ],

  "transactions": [
    {
      "code1C": "300200317_",
      "transactionId": "SALE-10023",
      "date": "2026-09-10",
      "warehouse": "Алматы",
      "quantity": 420
    },
    {
      "code1C": "300200317_",
      "transactionId": "SALE-10111",
      "date": "2026-09-14",
      "warehouse": "Алматы",
      "quantity": 1
    }
  ],

  "inventory": [
    {
      "code1C": "300200317_",
      "totalStock": 20,
      "reservedStock": 5,
      "freeStock": 15,
      "inTransit": 10
    }
  ],

  "seasonality": [
    {
      "month": 9,
      "coefficient": 0.892
    },
    {
      "month": 10,
      "coefficient": 0.954
    },
    {
      "month": 11,
      "coefficient": 0.982
    }
  ]
}
```

## Обязательные поля

| Поле            |    Тип | Правило                        |
| --------------- | -----: | ------------------------------ |
| `schemaVersion` | string | Сейчас всегда `1.0`            |
| `requestId`     | string | UUID, уникальный для расчёта   |
| `asOfDate`      | string | Формат `YYYY-MM-DD`            |
| `products`      |  array | Справочник товаров и MOQ       |
| `monthlySales`  |  array | Продажи по месяцам             |
| `monthlyStock`  |  array | Остатки по месяцам             |
| `transactions`  |  array | Детальные продажи для аномалий |
| `inventory`     |  array | Текущий остаток и товар в пути |
| `seasonality`   |  array | Коэффициенты сезонности        |
| `settings`      | object | Параметры расчёта              |

## Важные правила данных

* `code1C` всегда передаётся как `string`.
* Символ `_` в конце кода нельзя удалять.
* `month` имеет формат `YYYY-MM`.
* `quantity: 0` означает реальный ноль.
* `quantity: null` означает, что значение отсутствует.
* Отрицательные продажи допустимы: это возвраты или корректировки.
* `moq: null` означает, что кратность заказа неизвестна.
* `unitCost: null` означает, что стоимость неизвестна.
* Backend должен объединить дубли в месячных данных до отправки.
* Детальные транзакции объединять не нужно.

---

# 3. Успешный ответ AI Service → Backend

HTTP:

```http
200 OK
Content-Type: application/json
```

```json
{
  "schemaVersion": "1.0",
  "requestId": "93a40813-12dd-4aef-829d-4803251f6814",
  "runId": "run-20260922-001",
  "status": "COMPLETED_WITH_WARNINGS",
  "generatedAt": "2026-09-23T10:30:00Z",
  "processingTimeMs": 4812,

  "summary": {
    "totalProducts": 1,
    "buyCount": 0,
    "noBuyCount": 0,
    "reviewCount": 1,
    "totalRecommendedQuantity": 25,
    "estimatedOrderCost": 31250
  },

  "providers": {
    "nvidia": {
      "status": "USED",
      "reviewedAnomalies": 1
    },
    "openai": {
      "status": "USED",
      "generatedExplanations": 1
    }
  },

  "recommendations": [
    {
      "code1C": "300200317_",
      "article": "SE-317",
      "name": "Автоматический выключатель",
      "supplier": "SystemElectric",

      "action": "REVIEW",
      "urgency": "HIGH",
      "confidence": 0.78,
      "requiresManualReview": true,

      "recommendedQuantity": 25,
      "estimatedUnitCost": 1250,
      "estimatedCost": 31250,

      "calculation": {
        "historyPeriod": {
          "from": "2025-09",
          "to": "2026-08"
        },
        "baseMonthlyDemand": 21.33,
        "anomalyExcludedQuantity": 420,
        "stockoutCompensation": 3,
        "correctedMonthlyDemand": 24.33,
        "growthFactor": 1.08,
        "seasonalityFactor": 0.954,
        "forecastHorizonMonths": 2,
        "forecastDemand": 50.14,
        "safetyStock": 11.36,
        "freeStock": 15,
        "inTransit": 10,
        "availableStock": 25,
        "rawRequirement": 36.5,
        "moq": 5,
        "roundedRequirement": 40
      },

      "anomalyAnalysis": {
        "status": "PENDING_REVIEW",
        "candidates": [
          {
            "transactionId": "SALE-10023",
            "date": "2026-09-10",
            "quantity": 420,
            "medianTransactionQuantity": 10,
            "deviationRatio": 42,
            "nvidiaVerdict": "ONE_OFF",
            "confidence": 0.81,
            "systemDecision": "PENDING_REVIEW",
            "reason": "Продажа в 42 раза превышает обычный размер операции и не повторяется регулярно"
          }
        ]
      },

      "explanation": {
        "short": "Рекомендуется проверить заказ на 25 единиц перед подтверждением.",
        "details": [
          "Средний скорректированный спрос составляет 24.33 единицы в месяц.",
          "Учтены сезонность, рост продаж и страховой запас.",
          "Доступно 15 единиц на складе и 10 единиц в пути.",
          "Продажа на 420 единиц классифицирована как возможный разовый заказ."
        ],
        "generatedBy": "OPENAI"
      },

      "warnings": [
        {
          "code": "ANOMALY_REQUIRES_CONFIRMATION",
          "severity": "WARNING",
          "message": "Нужно подтвердить исключение крупной продажи на 420 единиц"
        },
        {
          "code": "PARTIAL_MONTH_EXCLUDED",
          "severity": "INFO",
          "message": "Неполный сентябрь 2026 года исключён из среднего спроса"
        }
      ]
    }
  ]
}
```

### Допустимые значения `action`

| Значение | Значение для интерфейса |
| -------- | ----------------------- |
| `BUY`    | Заказать                |
| `NO_BUY` | Не заказывать           |
| `REVIEW` | Проверить вручную       |

### Допустимые значения `urgency`

```text
HIGH
MEDIUM
LOW
NONE
```

### Статус аномалии NVIDIA

```text
ONE_OFF       — вероятно разовая крупная продажа
RECURRING     — регулярная оптовая продажа
UNCERTAIN     — невозможно определить
NOT_EVALUATED — NVIDIA не вызывался
```

### Решение системы по аномалии

```text
EXCLUDED        — исключена из прогноза
INCLUDED        — оставлена в истории
PENDING_REVIEW  — ожидает решения менеджера
```

Важно: NVIDIA только предлагает классификацию. Окончательное исключение спорной продажи подтверждает менеджер.

---

# 4. Ответ без предупреждений

```json
{
  "schemaVersion": "1.0",
  "requestId": "93a40813-12dd-4aef-829d-4803251f6814",
  "runId": "run-20260922-001",
  "status": "COMPLETED",
  "generatedAt": "2026-09-23T10:30:00Z",
  "processingTimeMs": 2950,
  "summary": {
    "totalProducts": 100,
    "buyCount": 42,
    "noBuyCount": 58,
    "reviewCount": 0,
    "totalRecommendedQuantity": 12500,
    "estimatedOrderCost": 24150000
  },
  "providers": {
    "nvidia": {
      "status": "SKIPPED",
      "reviewedAnomalies": 0
    },
    "openai": {
      "status": "USED",
      "generatedExplanations": 42
    }
  },
  "recommendations": []
}
```

Статусы всего расчёта:

```text
COMPLETED
COMPLETED_WITH_WARNINGS
FAILED
```

---

# 5. Если OpenAI или NVIDIA недоступны

AI Service не должен ронять весь расчёт.

```json
{
  "schemaVersion": "1.0",
  "requestId": "93a40813-12dd-4aef-829d-4803251f6814",
  "runId": "run-20260922-001",
  "status": "COMPLETED_WITH_WARNINGS",
  "generatedAt": "2026-09-23T10:30:00Z",
  "processingTimeMs": 2100,
  "summary": {
    "totalProducts": 1,
    "buyCount": 0,
    "noBuyCount": 0,
    "reviewCount": 1,
    "totalRecommendedQuantity": 25,
    "estimatedOrderCost": 31250
  },
  "providers": {
    "nvidia": {
      "status": "FAILED",
      "reviewedAnomalies": 0
    },
    "openai": {
      "status": "FALLBACK",
      "generatedExplanations": 0
    }
  },
  "recommendations": [],
  "warnings": [
    {
      "code": "NVIDIA_UNAVAILABLE",
      "severity": "WARNING",
      "message": "Аномалии требуют ручной проверки"
    },
    {
      "code": "OPENAI_UNAVAILABLE",
      "severity": "WARNING",
      "message": "Использованы шаблонные объяснения"
    }
  ]
}
```

Возможные статусы провайдеров:

```text
USED
SKIPPED
FALLBACK
FAILED
```

---

# 6. Формат ошибок

## Некорректный JSON

```http
400 Bad Request
```

```json
{
  "requestId": "93a40813-12dd-4aef-829d-4803251f6814",
  "error": {
    "code": "INVALID_REQUEST",
    "message": "Запрос содержит некорректный JSON",
    "fields": []
  }
}
```

## Ошибка данных

```http
422 Unprocessable Entity
```

```json
{
  "requestId": "93a40813-12dd-4aef-829d-4803251f6814",
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Невозможно выполнить расчёт",
    "fields": [
      {
        "field": "products[0].code1C",
        "message": "Поле обязательно"
      },
      {
        "field": "settings.forecastHorizonMonths",
        "message": "Значение должно быть больше нуля"
      }
    ]
  }
}
```

## Слишком большой запрос

```http
413 Payload Too Large
```

```json
{
  "requestId": "93a40813-12dd-4aef-829d-4803251f6814",
  "error": {
    "code": "PAYLOAD_TOO_LARGE",
    "message": "Размер запроса превышает 50 MB",
    "fields": []
  }
}
```

## Внутренняя ошибка

```http
500 Internal Server Error
```

```json
{
  "requestId": "93a40813-12dd-4aef-829d-4803251f6814",
  "error": {
    "code": "INTERNAL_ERROR",
    "message": "Не удалось сформировать рекомендации",
    "fields": []
  }
}
```

---

# 7. Health-check

```http
GET /health
```

Ответ:

```json
{
  "status": "ok",
  "service": "recommendation-ai-service",
  "version": "1.0.0",
  "dependencies": {
    "openai": "configured",
    "nvidia": "configured"
  }
}
```

---

# 8. Главные договорённости команды

1. Go Backend отвечает за чтение и проверку Excel.
2. Backend объединяет строки по точному `code1C`.
3. AI Service никогда не принимает `.xlsx`.
4. AI Service возвращает расчётные поля — Frontend ничего не пересчитывает.
5. LLM не придумывает количество заказа.
6. NVIDIA классифицирует только найденные системой аномалии.
7. OpenAI получает готовые цифры и пишет объяснение.
8. При падении OpenAI/NVIDIA формульный расчёт продолжает работать.
9. `REVIEW` может содержать предварительное `recommendedQuantity`, но такой заказ нельзя автоматически экспортировать без подтверждения.
10. Backend хранит пользовательское подтверждение отдельно — это не ответственность AI Service.
