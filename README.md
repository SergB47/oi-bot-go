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
