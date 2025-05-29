<img src="https://repository-images.githubusercontent.com/991835536/72ff229d-e789-4fc8-883d-53439aab3c0d" align="right" width="60%">

## 🇬🇧 `redis-backup` 
<a href="#-использование"> 🇷🇺 Инструкция на Русском</a>

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

## 📝 License

This project is licensed under the GNU General Public License (GPL).

---
