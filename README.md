## 🇬🇧 How to use `backup-redis`

### 📦 Overview

`backup-redis` is a standalone Go utility that automatically detects all running Redis instances on the server, identifies their RDB file paths via `CONFIG GET`, and creates compressed `.tar.gz` backups.

It also supports:

* Automatic detection of Redis ports
* Backup retention (daily/weekly/monthly/yearly)
* Interactive restore wizard
* Permission and ownership preservation
* Friendly color-coded terminal output

---

### 🚀 Usage

```bash
./backup-redis [flags]
```

#### Available Flags:

| Flag            | Description                                    | Default   |
| --------------- | ---------------------------------------------- | --------- |
| `--backup-path` | Root directory to store all backups            | `/backup` |
| `--days`        | Number of days to keep daily backups           | `30`      |
| `--list`        | Show all existing backups and exit             |           |
| `--restore`     | Launch interactive restore wizard              |           |
| `--help`        | Show help and display detected Redis instances |           |

---

### 🔍 Example

```bash
sudo ./backup-redis --backup-path /mnt/backups
```

Output:

```
✔ Redis 6379 → /var/lib/redis/dump.rdb
📦 Archiving /mnt/backups/<host>/redis_6379/daily/2025-05-28_13-10-00_redis_6379.tar.gz
💾 Archive size: 4.81 MB
```

To restore:

```bash
sudo ./backup-redis --restore
```

---

## 🇷🇺 Как пользоваться `backup-redis`

### 📦 Обзор

`backup-redis` — это автономная утилита на Go, которая автоматически находит все запущенные экземпляры Redis на сервере, определяет путь к их RDB-файлам через `CONFIG GET` и создает сжатые архивы `.tar.gz`.

Функции:

* Автоопределение портов Redis
* Хранение бэкапов: ежедневные, еженедельные, ежемесячные, годовые
* Интерактивный мастер восстановления
* Сохранение прав и владельцев файлов
* Удобный вывод в терминал с цветами и подсказками

---

### 🚀 Использование

```bash
./backup-redis [флаги]
```

#### Доступные флаги:

| Флаг            | Описание                                               | По умолчанию |
| --------------- | ------------------------------------------------------ | ------------ |
| `--backup-path` | Папка, в которую сохраняются все бэкапы                | `/backup`    |
| `--days`        | Сколько дней хранить ежедневные бэкапы                 | `30`         |
| `--list`        | Показать все существующие бэкапы и выйти               |              |
| `--restore`     | Запустить мастер восстановления                        |              |
| `--help`        | Показать справку и список обнаруженных Redis-инстансов |              |

---

### 🔍 Пример

```bash
sudo ./backup-redis --backup-path /mnt/backups
```

Вывод:

```
✔ Redis 6379 → /var/lib/redis/dump.rdb
📦 Архивирование /mnt/backups/<host>/redis_6379/daily/2025-05-28_13-10-00_redis_6379.tar.gz
💾 Размер архива: 4.81 MB
```

Для восстановления:

```bash
sudo ./backup-redis --restore
```

