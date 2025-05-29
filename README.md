<img src="https://repository-images.githubusercontent.com/991835536/72ff229d-e789-4fc8-883d-53439aab3c0d" align="right" width="50%">
## 🇬🇧 How to use `redis-backup` / <a href="?tab=readme-ov-file#-как-пользоваться-redis-backup"> 🇷🇺 Как пользоваться `redis-backup`</a>

### 📦 Overview

`redis-backup` is a standalone Go utility that automatically detects all running Redis instances on the server, identifies their RDB file paths via `CONFIG GET`, and creates compressed `.tar.gz` backups.

It also supports:

* Automatic detection of Redis ports
* Backup retention (daily/weekly/monthly/yearly)
* Interactive restore wizard
* Permission and ownership preservation
* Friendly color-coded terminal output
* Off-site replication to any FTP server (native client, remote retention × `--ftp-keep-factor`)
* Ability to skip selected Redis ports (`--exclude-ports`)
* Nagios-style integrity check (`--check`) for backup age and size

---

### 🚀 Usage

```bash
./redis-backup [flags]
```

#### Available Flags:

| Flag                | Description                                                                  | Default                |
| ------------------- | ---------------------------------------------------------------------------- | ---------------------- |
| `--backup-path`     | Root directory to store all backups                                          | `/backup`              |
| `--days`            | Number of days to keep daily backups locally                                 | `30`                   |
| `--list`            | Show all existing backups and exit                                           |                        |
| `--restore`         | Launch interactive restore wizard                                            |                        |
| `--help`            | Show help and display detected Redis instances                               |                        |
| `--exclude-ports`   | Comma-separated list of Redis ports to **exclude** from backup/check         |                        |
| `--check`           | Verify freshness/size – CRITICAL if last backup older than *N* hours or <75% |                        |
| `--ftp-conf`        | Path to FTP credentials file                                                 | `/etc/ftp-backup.conf` |
| `--ftp-host`        | Override FTP host (takes precedence over conf file)                          |                        |
| `--ftp-user`        | Override FTP username                                                        |                        |
| `--ftp-pass`        | Override FTP password                                                        |                        |
| `--ftp-keep-factor` | Store data on FTP **N ×** longer than locally                                | `4`                    |

---

### 🔍 Example

```bash
sudo ./redis-backup --backup-path /mnt/backups
```

Output:

```
✔ Redis 6379 → /var/lib/redis/dump.rdb
📦 Archiving /mnt/backups/<host>/redis_6379/daily/2025-05-28_13-10-00_redis_6379.tar.gz
💾 Archive size: 4.81 MB
⇪ Uploading to FTP: host872.your-backup.de:/<path>/redis_6379/…
```

To restore:

```bash
sudo ./redis-backup --restore
```

To run integrity check (for Nagios or CI):

```bash
redis-backup --check 24
```

---

## 🇬🇧 Install `redis-backup` (amd64)

🔧 **Linux (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_linux_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

🍏 **macOS (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_darwin_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

🦫 **OpenBSD (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_openbsd_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

🧢 **FreeBSD (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_freebsd_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

> After installation, run:
>
> ```bash
> redis-backup --help
> ```

---

## 🇷🇺 Как пользоваться `redis-backup`

### 📦 Обзор

`redis-backup` — это автономная утилита на Go, которая автоматически находит все запущенные экземпляры Redis на сервере, определяет путь к их RDB-файлам через `CONFIG GET` и создает сжатые архивы `.tar.gz`.

Поддерживает:

* Автоопределение портов Redis
* Хранение бэкапов: ежедневные, еженедельные, ежемесячные, годовые
* Интерактивный мастер восстановления
* Сохранение прав и владельцев файлов
* Удобный вывод в терминал с цветами и подсказками
* Репликация на FTP (автоматически, с увеличенным сроком хранения)
* Исключение портов Redis при бэкапе или проверке (`--exclude-ports`)
* Проверка свежести и размера бэкапа в стиле Nagios (`--check`)

---

### 🚀 Использование

```bash
./redis-backup [флаги]
```

#### Доступные флаги:

| Флаг                | Описание                                                                                      | По умолчанию           |
| ------------------- | --------------------------------------------------------------------------------------------- | ---------------------- |
| `--backup-path`     | Папка, в которую сохраняются все бэкапы                                                       | `/backup`              |
| `--days`            | Сколько дней хранить ежедневные бэкапы                                                        | `30`                   |
| `--list`            | Показать все существующие бэкапы и выйти                                                      |                        |
| `--restore`         | Запустить мастер восстановления                                                               |                        |
| `--help`            | Показать справку и список обнаруженных Redis-инстансов                                        |                        |
| `--exclude-ports`   | Список портов Redis через запятую, которые **не** нужно бэкапить/проверять                    |                        |
| `--check`           | Проверка свежести/размера – CRITICAL, если последний бэкап старше *N* часов или <75 % размера |                        |
| `--ftp-conf`        | Путь к файлу с FTP-учётными данными                                                           | `/etc/ftp-backup.conf` |
| `--ftp-host`        | Переопределить FTP-хост (приоритет выше конф-файла)                                           |                        |
| `--ftp-user`        | Переопределить FTP-логин                                                                      |                        |
| `--ftp-pass`        | Переопределить FTP-пароль                                                                     |                        |
| `--ftp-keep-factor` | Хранить данные на FTP в **N раз** дольше, чем локально                                        | `4`                    |

---

### 🔍 Пример

```bash
sudo ./redis-backup --backup-path /mnt/backups
```

Вывод:

```
✔ Redis 6379 → /var/lib/redis/dump.rdb
📦 Архивирование /mnt/backups/<host>/redis_6379/daily/2025-05-28_13-10-00_redis_6379.tar.gz
💾 Размер архива: 4.81 MB
⇪ Загрузка на FTP: host872.your-backup.de:/<path>/redis_6379/…
```

Для восстановления:

```bash
sudo ./redis-backup --restore
```

Проверка свежести и размера (для мониторинга или CI):

```bash
redis-backup --check 24
```

---

## 🇷🇺 Установка `redis-backup` (amd64)

🔧 **Linux (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_linux_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

🍏 **macOS (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_darwin_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

🦫 **OpenBSD (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_openbsd_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

🧢 **FreeBSD (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/download/latest/redis-backup_freebsd_amd64 -o /usr/local/bin/redis-backup && chmod +x /usr/local/bin/redis-backup
```

> После установки проверьте:
>
> ```bash
> redis-backup --help
> ```
