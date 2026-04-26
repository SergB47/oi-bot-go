# Hyperliquid OI Monitor

Мониторинг открытого интереса (Open Interest) и funding rate с биржи Hyperliquid.

## Функции

- Сбор данных Open Interest со всех перпетуальных DEXов (native + HIP-3)
- Отслеживание funding rate с расчетом годового APR
- Алерты при изменении OI >= 20%
- Алерты при изменении funding rate >= 20% (5 мин) или >= 10% (1 час)
- Уведомления в Telegram
- Хранение данных в SQLite

## Быстрый старт на VPS

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

### 2. Настройка Telegram бота

```bash
# Скопировать пример конфигурации
cp .env.example .env

# Отредактировать .env
nano .env
```

Заполнить в `.env`:
- `TELEGRAM_BOT_TOKEN` - токен от @BotFather
- `TELEGRAM_CHAT_ID` - ваш ID (получить от @userinfobot)

### 3. Запуск

```bash
# Однократный сбор данных
./oi_monitor -once

# Постоянная работа (каждые 5 минут)
./oi_monitor

# Только нативный DEX
./oi_monitor -native

# Просмотр истории
./oi_monitor -history BTC
./oi_monitor -dex-history xyz
```

### 4. Запуск через systemd (автозапуск)

```bash
sudo nano /etc/systemd/system/oi-monitor.service
```

Содержимое:
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

Активация:
```bash
sudo systemctl daemon-reload
sudo systemctl enable oi-monitor
sudo systemctl start oi-monitor
sudo systemctl status oi-monitor
```

## Команды

| Команда | Описание |
|---------|----------|
| `./oi_monitor` | Запуск сбора данных каждые 5 минут |
| `./oi_monitor -once` | Однократный сбор |
| `./oi_monitor -once -debug` | Тестовый запуск без Telegram |
| `./oi_monitor -native` | Только нативный DEX |
| `./oi_monitor -debug` | Режим отладки (без Telegram бота) |
| `./oi_monitor -history BTC` | История по монете |
| `./oi_monitor -dex-history xyz` | История по DEX |
| `./oi_monitor -alerts` | Показать алерты OI |
| `./oi_monitor -list-dexes` | Список DEXов |

## Режим отладки (`-debug`)

Используйте `-debug` для тестирования без Telegram бота:
- Быстрый тест без подключения к боту
- Можно запускать несколько копий одновременно
- Нет конфликтов при подключении к боту

```bash
./oi_monitor -once -debug
./oi_monitor -debug -native
```

## Файлы

- `oi_monitor.db` - база данных SQLite
- `.env` - конфигурация Telegram
- `oi_monitor` - бинарник

## Структура базы

Таблицы:
- `oi_history` - история OI и funding
- `alerts` - алерты изменения OI
- `funding_alerts` - алерты изменения funding
- `instrument_stats` - статистика по инструментам (14-day rolling)
- `signal_queue` - очередь сигналов для digest

---

## Smart Alerting System (Momentum Strategy)

Система интеллектуального обнаружения импульсов движения открытого интереса (OI) с определением направления (LONG/SHORT) и уровня уверенности.

### Стратегия обнаружения (Front-running Detection)

**Гипотеза:** Инсайдеры торгуют в перпетуальных контрактах перед появлением публичной информации, вызывая рост OI, в то время как спот-цена еще не двинулась. Система торгует ВМЕСТЕ с этим импульсом, а не против него.

**Логика сигнала:**

| Условие | Сигнал | Обоснование |
|---------|--------|-------------|
| OI ↑ + Funding ↑ (положительный и растущий) | 🟩 **LONG** | Открывается больше лонгов, готовых платить премию = бычий фронтраннинг |
| OI ↑ + Funding ↓ (отрицательный и падающий) | 🟥 **SHORT** | Открывается больше шортов, готовых платить премию = медвежий фронтраннинг |
| OI ↑ + Funding устарел/плоский + Цена ↑ | 🟩 **LONG** | Цена растет с OI = накопление лонгов |
| OI ↑ + Funding устарел/плоский + Цена ↓ | 🟥 **SHORT** | Цена падает с OI = накопление шортов |

**Funding как подтверждение:**
- Положительный funding (> 0) = Лонги платят шортам = больше лонг позиций = подтверждение LONG
- Отрицательный funding (< 0) = Шорты платят лонгам = больше шорт позиций = подтверждение SHORT
- Устаревший funding (> 50 мин) = Использовать направление цены как прокси

### Уровни уверенности (Confidence Levels)

| Уровень | Условия |
|---------|---------|
| 🔥 **HIGH** | Свежий funding (< 10 мин) с четким направлением |
| 🟡 **MEDIUM** | Устаревший funding, но цена подтверждает направление |
| ⚪ **LOW** | Конфликтующие сигналы или недостаточно данных |

### CLI флаги Smart Alerting

| Флаг | По умолчанию | Описание |
|------|--------------|----------|
| `-smart-alerts` | `true` | Включить momentum-based alerting |
| `-instant-threshold` | `30.0` | Порог для мгновенных алертов (%) |
| `-digest-interval` | `30m` | Интервал отправки digest |
| `-night-start` | `22` | Начало ночного режима (час) |
| `-night-end` | `8` | Конец ночного режима (час) |

### Примеры запуска

```bash
# Smart alerting с настройками по умолчанию
./oi_monitor

# Отключить smart alerting, использовать legacy режим
./oi_monitor -smart-alerts=false

# Настроить порог мгновенных алертов
./oi_monitor -instant-threshold 25.0

# Изменить интервал digest
./oi_monitor -digest-interval 15m

# Настроить ночной режим
./oi_monitor -night-start 23 -night-end 7
```

### Формат выходного сигнала

**Пример instant alert:**

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
Confidence: HIGH (fresh funding + price agreement)
```

**Индикаторы свежести funding:**
- 🆕 = Обновлен < 10 мин назад (свежий, высокая уверенность)
- ⏱️ = Обновлен 10-30 мин назад (недавний, средняя уверенность)
- ⏳ = Обновлен 30-50 мин назад (устаревает, проверять цену)
- ⚠️ = Обновлен > 50 мин назад (устаревший, только цена)

### Режимы работы

**Instant Mode:** Срабатывает при изменении OI > 30% или Z-score > 3. Отправляет немедленный alert.

**Periodic Mode (30 min):** Анализирует окна 30 мин / 2 часа / 24 часа. Группирует сигналы в digest.

**Night Mode (22:00-08:00):** Отправляет только экстремальные сигналы (score > 85, OI > 50%) с высокой уверенностью.
