<img src="https://repository-images.githubusercontent.com/991835536/72ff229d-e789-4fc8-883d-53439aab3c0d" align="right" width="60%">

## 🇬🇧 `redis-backup` 
<a href="#-использование">🇷🇺 Инструкция на Русском</a>

## English

### 📦 Overview

`redis-backup` is a standalone Go utility that automatically finds all running Redis instances on the host, detects their `RDB` file locations using `CONFIG GET`, and creates compressed `.tar.gz` backups.

#### Features:

* Auto-discovery of Redis ports
* Retention support (daily, weekly, monthly, yearly)
* Interactive restore wizard
* File ownership and permission preservation
* Colorized terminal output
* FTP upload with retention multiplication (`--ftp-keep-factor`)
* Ability to exclude specific Redis ports (`--exclude-ports`)
* Nagios-style check mode (`--check`) for:

  * Backup freshness and size
  * Disk usage and free space estimation

#### Example Nagios command:

```bash
/usr/local/bin/check_ssh r2d2@$HOSTADDRESS$ sudo /usr/local/bin/redis-backup --check $ARG1
```

---

### 🚀 Usage

```bash
redis-backup [flags]
```

| Flag                | Description                                                         | Default                |
| ------------------- | ------------------------------------------------------------------- | ---------------------- |
| `--backup-path`     | Root folder for backups                                             | `/backup`              |
| `--days`            | How many days to keep daily backups                                 | `30`                   |
| `--list`            | List existing backups and exit                                      |                        |
| `--restore`         | Start interactive restore wizard                                    |                        |
| `--help`            | Print help and show detected Redis instances                        |                        |
| `--exclude-ports`   | Comma-separated list of Redis ports to ignore                       |                        |
| `--check`           | Check freshness/size/disk space – CRITICAL if outdated or too small |                        |
| `--ftp-conf`        | FTP credentials file path                                           | `/etc/ftp-backup.conf` |
| `--ftp-host`        | Override FTP host                                                   |                        |
| `--ftp-user`        | Override FTP username                                               |                        |
| `--ftp-pass`        | Override FTP password                                               |                        |
| `--ftp-keep-factor` | Store backups on FTP `N×` longer than locally                       | `4`                    |

---

### 🔧 Installation

Download for your platform and move to `/usr/local/bin/`:

**Linux (amd64):**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_linux_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

**macOS (amd64):**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_darwin_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

**OpenBSD (amd64):**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_openbsd_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```


**FreeBSD (amd64):**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_freebsd_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

---

Here’s the English translation of your minimalistic guide:

---

## 📦 Redis Backup

```bash
sudo redis-backup
```

🔧 By default:

* Backups are saved to:
  `/backup/<hostname>/redis-backup/redis_<port>/daily/`
* Archive name format:
  `YYYY-MM-DD_HH-MM-SS_redis_<port>.tar.gz`
* Weekly, monthly, and yearly copies are created automatically.

📁 Example directory structure:

```
/backup/
└── my-server/
    └── redis-backup/
        ├── redis_6379/
        │   ├── daily/
        │   ├── weekly/
        │   └── ...
        └── redis_6380/
            └── ...
```

---

## 🔄 Restore

```bash
sudo redis-backup --restore
```

Interactive menu:

1. Choose Redis port (e.g., 6379)
2. Choose archive
3. Confirm restore

📌 During restore:

* The current RDB file is renamed to `.backup`
* The selected archive is extracted into the same directory
* File permissions are preserved

---

## 📁 Where Backups Are Stored

Locally:

```
/backup/<hostname>/redis-backup/redis_<port>/daily/*.tar.gz
```

On FTP (if enabled):

```
<hostname>/redis-backup/redis_<port>/daily/*.tar.gz
```

---

## 📋 Example Commands

### Backup Redis, excluding port 6400:

```bash
sudo redis-backup --exclude-ports 6400
```

### Freshness check (e.g., 24 hours) for Nagios:

```bash
redis-backup --check 24
```

---

## ⚙️ FTP Setup

File: `/etc/ftp-backup.conf`:

```
FTP_HOST=ftp.example.com
FTP_USER=myuser
FTP_PASS=mypass
```

---

## 🧼 Auto-cleanup

* Locally: old daily archives are deleted after `--days` days (default: 30).
* On FTP: files are deleted after `days × ftp-keep-factor` (default: ×4 = 120 days).

---


## Русский

### 📦 Обзор

`redis-backup` — это самостоятельная утилита на Go, которая автоматически находит все активные экземпляры Redis на сервере, определяет путь к их `RDB`-файлам с помощью `CONFIG GET` и создает сжатые `.tar.gz` архивы.

#### Возможности:

* Автоматическое определение портов Redis
* Хранение бэкапов (день, неделя, месяц, год)
* Интерактивный мастер восстановления
* Сохранение прав доступа и владельцев
* Удобный цветной вывод в терминал
* Отправка на FTP с увеличенным сроком хранения (`--ftp-keep-factor`)
* Исключение портов Redis (`--exclude-ports`)
* Проверка свежести и размера бэкапов (`--check`) в стиле Nagios:

  * Проверка актуальности и размера бэкапа
  * Проверка дискового пространства и прогноза заполнения

#### Пример команды для Nagios:

```bash
/usr/local/bin/check_ssh r2d2@$HOSTADDRESS$ sudo /usr/local/bin/redis-backup --check $ARG1
```

---

### 🚀 Использование

```bash
redis-backup [флаги]
```

| Флаг                | Описание                                                              | Значение по умолчанию  |
| ------------------- | --------------------------------------------------------------------- | ---------------------- |
| `--backup-path`     | Папка для хранения всех бэкапов                                       | `/backup`              |
| `--days`            | Сколько дней хранить ежедневные бэкапы                                | `30`                   |
| `--list`            | Показать существующие бэкапы и выйти                                  |                        |
| `--restore`         | Запустить мастер восстановления                                       |                        |
| `--help`            | Показать справку и список Redis-инстансов                             |                        |
| `--exclude-ports`   | Список портов Redis, которые не нужно бэкапить                        |                        |
| `--check`           | Проверка свежести/размера/места – CRITICAL, если бэкап старый или мал |                        |
| `--ftp-conf`        | Путь к файлу с FTP-учётными данными                                   | `/etc/ftp-backup.conf` |
| `--ftp-host`        | Переопределить FTP-хост                                               |                        |
| `--ftp-user`        | Переопределить FTP-логин                                              |                        |
| `--ftp-pass`        | Переопределить FTP-пароль                                             |                        |
| `--ftp-keep-factor` | Хранить на FTP в `N` раз дольше, чем локально                         | `4`                    |

---

### 🔧 Установка

Скачайте и поместите в `/usr/local/bin/`:

**Linux (amd64):**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_linux_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

**macOS (amd64):**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_darwin_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```
**OpenBSD (amd64):**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_openbsd_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

**FreeBSD (amd64):**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_freebsd_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

---

## 📦 Резервное копирование Redis

sudo redis-backup

🔧 По умолчанию:

* бэкапы сохраняются в:
  /backup/<hostname>/redis-backup/redis_<порт>/daily/
* название архива:
  YYYY-MM-DD_HH-MM-SS_redis_<порт>.tar.gz
* также автоматически создаются еженедельные, ежемесячные и годовые копии.

📁 Пример расположения:

```
/backup/
└── my-server/
    └── redis-backup/
        ├── redis_6379/
        │   ├── daily/
        │   ├── weekly/
        │   └── ...
        └── redis_6380/
            └── ...
```
---

## 🔄 Восстановление

sudo redis-backup --restore

Интерактивное меню:

1. Выбор порта Redis (например, 6379)
2. Выбор нужного архива
3. Подтверждение восстановления

📌 При восстановлении:

* текущий RDB-файл переименовывается в .backup
* новый файл распаковывается из архива в ту же папку
* права доступа сохраняются

---

## 📁 Где лежат бэкапы

Локально:

/backup/<hostname>/redis-backup/redis_<port>/daily/*.tar.gz

На FTP (если включено):

<hostname>/redis-backup/redis_<port>/daily/*.tar.gz

---

## 📋 Примерные команды

### Бэкап Redis, кроме портов 6400:

sudo redis-backup --exclude-ports 6400

### Проверка свежести (например, 24 часа) для Nagios:

redis-backup --check 24

---

## ⚙️ Настройка FTP

Файл /etc/ftp-backup.conf:

FTP_HOST=ftp.example.com
FTP_USER=myuser
FTP_PASS=mypass

---

## 🧼 Автоудаление

* Локально: старые daily-архивы удаляются через --days дней (по умолчанию 30).
* На FTP: удаление производится по формуле days × ftp-keep-factor (по умолчанию ×4 = 120 дней).

---

## 📝 License

This project is licensed under the GNU General Public License (GPL).

---
