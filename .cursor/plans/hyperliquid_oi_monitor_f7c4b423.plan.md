---
name: Hyperliquid OI Monitor
overview: Go-программа для мониторинга открытого интереса (Open Interest) с биржи Hyperliquid с периодическим сбором данных, сохранением в SQLite и алертами при значительных изменениях
todos:
  - id: "1"
    content: Создать структуру проекта и go.mod
    status: completed
  - id: "2"
    content: Реализовать типы данных для API ответа
    status: completed
  - id: "3"
    content: Реализовать HTTP клиент для Hyperliquid API
    status: completed
  - id: "4"
    content: Реализовать main.go с выводом таблицы Open Interest
    status: completed
  - id: "5"
    content: Протестировать программу
    status: completed
  - id: "6"
    content: Добавить SQLite хранилище с миграциями (таблица oi_history)
    status: completed
  - id: "7"
    content: Создать таблицу alerts для записи значительных изменений OI
    status: completed
  - id: "8"
    content: Реализовать планировщик сбора данных каждые 5 минут
    status: completed
  - id: "9"
    content: Реализовать логику сравнения OI и генерации алертов при изменении >= 20%
    status: completed
  - id: "10"
    content: Интегрировать все компоненты и протестировать
    status: completed
isProject: false
---

## Архитектура программы

### Структура проекта
```
oi_bot_go/
├── cmd/
│   └── monitor/
│       └── main.go              # Точка входа, инициализация
├── internal/
│   ├── hyperliquid/
│   │   ├── client.go            # HTTP клиент для API
│   │   └── types.go             # Структуры данных API
│   ├── storage/
│   │   ├── db.go                # Инициализация SQLite подключения
│   │   ├── migrations.go        # SQL миграции (oi_history, alerts)
│   │   └── repository.go        # Методы сохранения и чтения данных
│   └── scheduler/
│       └── scheduler.go           # Планировщик сбора данных каждые 5 минут
├── go.mod
└── oi_monitor.db                # SQLite база данных
```

### API Endpoint
- **URL**: `POST https://api.hyperliquid.xyz/info`
- **Body**: `{"type": "metaAndAssetCtxs"}`
- **Response**: массив с метаданными и контекстами активов, включая `openInterest` для каждого перпетуального контракта

### Схема базы данных

#### Таблица `oi_history`
Сохраняет все собранные данные раз в 5 минут:
| Поле | Тип | Описание |
|------|-----|----------|
| id | INTEGER PRIMARY KEY | Автоинкремент |
| coin | TEXT | Название актива (BTC, ETH, etc.) |
| open_interest | REAL | Значение OI |
| mark_price | REAL | Цена маркировки |
| funding | REAL | Ставка финансирования |
| timestamp | DATETIME | Время записи |
| created_at | DATETIME DEFAULT CURRENT_TIMESTAMP |

#### Таблица `alerts`
Записывает значительные изменения OI (>= 20%):
| Поле | Тип | Описание |
|------|-----|----------|
| id | INTEGER PRIMARY KEY | Автоинкремент |
| coin | TEXT | Название актива |
| previous_oi | REAL | Предыдущее значение OI |
| current_oi | REAL | Текущее значение OI |
| change_percent | REAL | Процент изменения |
| direction | TEXT | 'increase' или 'decrease' |
| timestamp | DATETIME | Время обнаружения |
| created_at | DATETIME DEFAULT CURRENT_TIMESTAMP |

### Компоненты

1. **HTTP Client** (`internal/hyperliquid/client.go`)
   - Метод `GetOpenInterest()` для запроса данных
   - Таймауты и базовая обработка ошибок
   - Content-Type: application/json

2. **Storage Layer** (`internal/storage/`)
   - `db.go` - подключение к SQLite (`oi_monitor.db`)
   - `migrations.go` - создание таблиц `oi_history` и `alerts`
   - `repository.go` - методы:
     - `SaveOIHistory(coin, openInterest, markPrice, funding)` - сохранение в oi_history
     - `GetLastOI(coin)` - получение последнего значения OI для сравнения
     - `SaveAlert(coin, prevOI, currentOI, changePercent, direction)` - сохранение алерта

3. **Scheduler** (`internal/scheduler/scheduler.go`)
   - `Start(interval)` - запуск бесконечного цикла с указанным интервалом
   - Каждые 5 минут:
     1. Получает данные от API
     2. Для каждого актива:
        - Получает последнее значение OI из базы
        - Если есть предыдущее значение:
          - Вычисляет процент изменения: `(current - previous) / previous * 100`
          - Если `abs(change) >= 20%` → записывает в таблицу `alerts`
        - Сохраняет текущее значение в `oi_history`

4. **Main** (`cmd/monitor/main.go`)
   - Инициализация базы данных (миграции)
   - Создание HTTP клиента
   - Запуск планировщика с интервалом 5 минут
   - Graceful shutdown (обработка сигналов SIGINT/SIGTERM)

### Логика алертинга
```
Для каждого актива:
  current_OI = получить от API
  last_OI = получить последнее из БД
  
  если last_OI существует:
    change = (current_OI - last_OI) / last_OI * 100
    
    если abs(change) >= 20%:
      direction = change > 0 ? "increase" : "decrease"
      записать в alerts(coin, last_OI, current_OI, change, direction)
  
  записать в oi_history(coin, current_OI, mark_price, funding)
```

### Зависимости
- Стандартная библиотека: `net/http`, `encoding/json`, `database/sql`, `time`, `context`
- Внешняя: `modernc.org/sqlite` (драйвер SQLite для Go без CGO)

### Интервалы
- Период сбора данных: **5 минут**
- Порог алертинга: **>= 20%** изменение OI