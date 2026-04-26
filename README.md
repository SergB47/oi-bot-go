# Hyperliquid OI Monitor

Мониторинг открытого интереса (Open Interest) и funding rate с биржи Hyperliquid с интеллектуальной системой обнаружения торговых сигналов.

## Возможности

- 📊 **Сбор данных** со всех перпетуальных DEXов (native + HIP-3)
- 🧠 **Smart Alerting** — детекция импульсов движения OI с определением направления (LONG/SHORT)
- 📈 **Анализ трендов** — многооконный анализ (30 мин, 2 часа, 24 часа)
- 🎯 **Scoring system** — оценка силы сигнала от 0 до 100
- 🌙 **Night Mode** — фильтрация сигналов ночью (22:00-08:00)
- 💬 **Telegram уведомления** — сводки каждые 30 минут + мгновенные алерты
- 💾 **SQLite хранилище** — полная история данных

---

## Алгоритм Smart Alerting

### Стратегия: Обнаружение фронтраннинга

**Гипотеза:** Инсайдеры торгуют в перпетуальных контрактах перед публикацией важной информации. Это вызывает рост OI, в то время как спот-цена еще не отреагировала. Мы торгуем ВМЕСТЕ с этим импульсом.

### Определение направления

| Сигнал | Условия | Логика |
|--------|---------|--------|
| 🟩 **LONG** | OI растет + Funding положительный | Инсайдеры открывают лонги, готовые платить премию |
| 🟥 **SHORT** | OI растет + Funding отрицательный | Инсайдеры открывают шорты, готовые платить премию |
| 🟩 **LONG*** | OI растет + Цена растет (Funding устарел) | Цена подтверждает накопление лонгов |
| 🟥 **SHORT*** | OI растет + Цена падает (Funding устарel) | Цена подтверждает накопление шортов |

*Fallback режим, когда funding не обновлялся > 10 минут

### Уровни уверенности (Confidence)

| Уровень | Индикатор | Описание |
|---------|-----------|----------|
| 🔥 **High** | Свежий funding (< 10 мин) с четким направлением |
| 🟡 **Medium** | Funding устарел, но цена подтверждает направление |
| ⚪ **Low** | Конфликтующие сигналы или недостаточно данных |

---

## Расчет Score (Силы сигнала)

Каждый сигнал получает оценку от 0 до 100 на основе 5 компонентов:

### Компоненты score:

```
Score = Speed (0-35) + Trend (0-25) + Funding (0-20) + Agreement (0-10) + Size (0-10)
```

| Компонент | Формула | Описание |
|-----------|---------|----------|
| **Speed** | min(\|OI 30m\| × 2, 35) | Скорость изменения OI за 30 минут |
| **Trend** | min(\|OI 2h\| × 1.5, 25) | Направленность тренда за 2 часа |
| **Funding** | min(\|ΔFunding\| × 0.5, 20) или min(\|Z-score\| × 5, 10) | Подтверждение через funding rate |
| **Agreement** | min(\|ΔЦены\| × 2, 10) | Бонус когда OI и цена движутся вместе |
| **Size** | min(log₁₀(OI_USD/1M) × 2, 10) | Бонус для крупных рынков |

### Интерпретация score:

| Score | Уровень | Действие |
|-------|---------|----------|
| 90-100 | Экстремальный | Мгновенный алерт + приоритет в дайджесте |
| 70-89 | Сильный | Высокий приоритет в дайджесте |
| 50-69 | Умеренный | Включается в дайджест |
| <50 | Слабый | Фильтруется (не отправляется) |

### Пример расчета:

**BTC:**
- OI +35% за 30 мин → 35 × 2 = 70, capped at **35**
- OI +42% за 2 часа → 42 × 1.5 = 63, capped at **30** → **25** (max)
- Funding изменился на +15% APR, свежий → 15 × 0.5 = 7.5, but we use Z-score 3.2 → min(3.2 × 5, 20) = **16** (no, fresh uses %change)
  - Actually: fresh funding uses %change: min(15 × 0.5, 20) = **7.5** → **7.5**... let me recalculate
  - Correction: if funding APR went from 10% to 28%, change = 180% (not 15%)
  - Let's say funding rate changed from 0.0001 to 0.0003 (200% change)
  - min(200 × 0.5, 20) = **20**
- Price +2.4%, OI растет → согласование → min(2.4 × 2, 10) = **4.8**
- OI_USD = $2.8B → log₁₀(2800) × 2 = 6.9 → **6.9**

**Total: 35 + 25 + 20 + 4.8 + 6.9 = 91.7** — Сильный сигнал на LONG

---

## Режимы работы

### Instant Mode (Мгновенный)

**Триггеры:**
- Изменение OI > 30% за 30 мин или 2 часа
- Funding Z-score > 3 (3 стандартных отклонения от нормы)
- Изменение funding > 50% (если свежий)

**Действие:** Немедленная отправка алерта в Telegram (с rate limit 15 мин на инструмент)

### Periodic Mode (Периодический)

**Триггеры:**
- Изменение OI > 15% за 30 мин
- Изменение OI > 20% за 2 часа
- Изменение OI > 30% за 24 часа
- Funding Z-score > 2

**Действие:** Сигнал ставится в очередь, отправляется в дайджесте каждые 30 минут

### Night Mode (Ночной)

**Время:** 22:00 — 08:00

**Фильтрация:**
- Только score > 85
- Только изменение OI > 50%
- Только высокая уверенность (свежий funding)

---

## Формат уведомлений

### Instant Alert (Критический импульс)

```
🚨 CRITICAL MOMENTUM | 14:32 UTC

🔥 BTC/native: LONG impulse accelerating
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
OI: +35% in 5 min ($2.1B → $2.8B)
Funding: +15% → +28% APR 🆕 (just updated!)
Price: +2.4% (mark) vs +2.1% (oracle)

Classification: 🟩 LONG IMPULSE | HIGH CONFIDENCE

Context:
• 30m:  +35% | 2h:  +42% | 24h:  +68%
• Funding Z-score: +3.2σ (extreme bullish)
• Mark/Oracle premium: +0.3% (longs paying up)

Signal strength: 97/100
```

### Digest (Сводка, каждые 30 мин)

```
🚀 Momentum Signals | 14:30 UTC | 12 signals

📊 Market Context:
Active: 47 instruments | 🟩 8 | 🟥 4

🟩 LONG IMPULSE:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔥 HIGH CONFIDENCE:
• BTC/native: OI +35% 30m (+42% 2h) | Funding +28% APR 🆕
  └ $2.8B | Price +2.1% | Score: 97 | high

🟡 MEDIUM CONFIDENCE:
• SOL/native: OI +22% 30m (+24% 2h)
  └ $1.1B | Price +1.5% | Score: 72 | medium

🟥 SHORT IMPULSE:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
🔥 HIGH CONFIDENCE:
• DOGE/native: OI +45% 30m | Funding -35% APR 🆕
  └ $174M | Price -3.2% | Score: 92 | high

📈 Top OI Movers (24h):
1. BTC: +68% 🟩  2. DOGE: +55% 🟥  3. ETH: +42% 🟩

🕐 Next digest: 15:00 UTC
```

### Индикаторы свежести funding:

- 🆕 — Обновлен < 10 минут назад (высокая уверенность)
- ⏱️ — Обновлен 10-30 минут назад (средняя уверенность)
- ⏳ — Обновлен 30-50 минут назад (проверять цену)
- ⚠️ — Обновлен > 50 минут назад (только цена)

---

## Установка и запуск

### 1. Установка

```bash
# Клонировать репозиторий
git clone https://github.com/yourusername/oi_bot_go.git
cd oi_bot_go

# Установить Go (если нет)
# Ubuntu/Debian:
sudo apt update && sudo apt install golang-go

# Собрать
go mod tidy
go build -o oi_monitor ./cmd/monitor
```

### 2. Настройка Telegram

```bash
# Скопировать пример конфигурации
cp .env.example .env

# Отредактировать .env
nano .env
```

Заполните в `.env`:
- `TELEGRAM_BOT_TOKEN` — получите от [@BotFather](https://t.me/BotFather)
- `TELEGRAM_CHAT_ID` — получите от [@userinfobot](https://t.me/userinfobot)

### 3. Запуск

```bash
# Постоянная работа (Smart Alerting включен по умолчанию)
./oi_monitor

# Однократный сбор для теста
./oi_monitor -once -debug

# Настроить порог мгновенных алертов
./oi_monitor -instant-threshold 25.0

# Изменить интервал дайджеста
./oi_monitor -digest-interval 15m

# Настроить ночной режим
./oi_monitor -night-start 23 -night-end 7

# Отключить Smart Alerting (legacy режим)
./oi_monitor -smart-alerts=false
```

### 4. Запуск через systemd

```bash
sudo nano /etc/systemd/system/oi-monitor.service
```

```ini
[Unit]
Description=Hyperliquid OI Monitor
After=network.target

[Service]
Type=simple
User=your_username
WorkingDirectory=/home/your_username/oi_bot_go
ExecStart=/home/your_username/oi_bot_go/oi_monitor
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable oi-monitor
sudo systemctl start oi-monitor
sudo systemctl status oi-monitor
```

---

## CLI команды

### Основные флаги

| Флаг | По умолчанию | Описание |
|------|--------------|----------|
| `./oi_monitor` | — | Запуск сбора каждые 5 минут |
| `-once` | — | Однократный сбор и выход |
| `-debug` | — | Режим отладки (без Telegram) |
| `-native` | — | Только native DEX (без HIP-3) |

### Smart Alerting флаги

| Флаг | По умолчанию | Описание |
|------|--------------|----------|
| `-smart-alerts` | `true` | Включить интеллектуальные сигналы |
| `-instant-threshold` | `30.0` | Порог для мгновенных алертов (%) |
| `-digest-interval` | `30m` | Интервал отправки сводки |
| `-night-start` | `22` | Начало ночного режима (час) |
| `-night-end` | `8` | Конец ночного режима (час) |

### Информационные команды

| Команда | Описание |
|---------|----------|
| `-history BTC` | История OI для монеты (24 часа) |
| `-dex-history xyz` | История OI для DEX (24 часа) |
| `-alerts` | Показать последние алерты OI |
| `-list-dexes` | Список всех DEXов в базе |

### Примеры

```bash
# Тестовый запуск без Telegram
./oi_monitor -once -debug

# Только крупные импульсы (>25%)
./oi_monitor -instant-threshold 25.0

# Частые дайджесты (каждые 15 мин)
./oi_monitor -digest-interval 15m

# Проверить историю BTC
./oi_monitor -history BTC

# Посмотреть активность DEX xyz (акции)
./oi_monitor -dex-history xyz
```

---

## Структура базы данных

### Таблицы

| Таблица | Назначение |
|---------|------------|
| `oi_history` | История OI, funding, цен для всех инструментов |
| `instrument_stats` | 14-дневная статистика (для Z-score) |
| `instrument_sync_state` | Состояние синхронизации (freshness tracking) |
| `signal_queue` | Очередь сигналов для дайджеста |
| `alerts` | Легаси алерты (простые OI изменения) |
| `funding_alerts` | Легаси алерты (funding изменения) |

### Полезные SQL запросы

```sql
-- Последние данные по BTC
SELECT * FROM oi_history 
WHERE coin = 'BTC' ORDER BY timestamp DESC LIMIT 5;

-- Топ изменений OI за час
SELECT coin, dex, 
       (open_interest_usd - LAG(open_interest_usd) OVER (PARTITION BY coin, dex ORDER BY timestamp)) / 
       LAG(open_interest_usd) OVER (PARTITION BY coin, dex ORDER BY timestamp) * 100 as change_pct
FROM oi_history 
WHERE timestamp > datetime('now', '-1 hour')
ORDER BY ABS(change_pct) DESC LIMIT 10;

-- Сигналы в очереди (не отправленные)
SELECT coin, dex, signal_direction, composite_score, signal_confidence
FROM signal_queue 
WHERE processed = 0 
ORDER BY composite_score DESC;

-- Статистика по инструменту (для Z-score)
SELECT * FROM instrument_stats WHERE coin = 'BTC' AND dex = 'native';
```

---

## Архитектура

```
cmd/monitor/
└── main.go              # Точка входа, CLI флаги

internal/
├── analyzer/            # Интеллектуальный анализ
│   ├── direction.go     # Определение LONG/SHORT
│   ├── stats.go         # 14-дневная статистика
│   ├── multi_window.go  # Анализ 30m/2h/24h
│   └── scorer.go        # Расчет score 0-100
├── hyperliquid/         # API клиент
├── scheduler/           # Планировщики
│   ├── scheduler.go     # Legacy планировщик
│   └── momentum_scheduler.go  # Smart планировщик
├── storage/             # База данных
└── telegram/            # Telegram бот
    └── digest_builder.go  # Форматирование сообщений
```

---

## Полезные ссылки

- **Hyperliquid API:** https://hyperliquid.gitbook.io/hyperliquid-docs/
- **Telegram Bot API:** https://core.telegram.org/bots/api
- **BotFather:** https://t.me/BotFather
- **User Info Bot:** https://t.me/userinfobot

---

## Лицензия

MIT License

---

*Последнее обновление: 2026-04-26*  
*Версия: Smart Alerting System v2.0*
