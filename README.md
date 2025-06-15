<img src="https://repository-images.githubusercontent.com/991835536/72ff229d-e789-4fc8-883d-53439aab3c0d" align="right" width="50%">

# 🇬🇧 redis-backup

[🇷🇺 Читать по-русски](#-русская-инструкция)

---

## 📦 Overview

**redis-backup** is a simple yet powerful Go utility:
- It automatically discovers all running Redis instances.
- Backs up their `RDB` files as compressed `.tar.gz` archives.
- Can replicate backups to **multiple FTP servers** for redundancy.
- Controls how many copies to keep locally and remotely.
- Includes an interactive restore wizard.
- Supports a robust check mode for monitoring freshness, size, disk usage, and FTP health.
- Locks itself to avoid parallel runs.
- Provides clear, colorized logs.

---

## ✅ Key Features

- 🔍 **Auto-discover Redis ports**
- 📂 **Multiple FTP upload** — replicate to as many FTPs as you want.
- 🔁 **Smart retention** — limit local copies (`--copies`) and multiply retention for FTP (`--ftp-keep-factor`).
- 🕵️ **Nagios-friendly check mode** — verify freshness, size, disk status and FTP consistency.
- 🔄 **Safe interactive restore**
- 🔐 **File lock to prevent overlaps**

---

## ⚙️ New Flags

| Flag                  | Description                                                       | Default |
| --------------------- | ----------------------------------------------------------------- | ------- |
| `--copies`, `-c`      | Max daily archives to keep locally (0 = unlimited)                | `0`     |
| `--ftp-keep-factor`   | Remote retention multiplier (`copies × factor` per FTP server)    | `4`     |

---

## 🗂️ FTP Configuration

You can define multiple FTP accounts in `/etc/ftp-backup.conf`:

```ini
# Example /etc/ftp-backup.conf

FTP_HOST=ftp1.example.com
FTP_USER=user1
FTP_PASS=pass1

FTP_HOST=ftp2.backup.net
FTP_USER=user2
FTP_PASS=pass2
````

Backups will be uploaded to **each** FTP defined.

---

## 🚀 Installation

**✅ Linux (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/latest/download/redis-backup_linux_amd64 -o /usr/local/bin/redis-backup
chmod +x /usr/local/bin/redis-backup
```

**✅ macOS (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/latest/download/redis-backup_darwin_amd64 -o /usr/local/bin/redis-backup
chmod +x /usr/local/bin/redis-backup
```

**✅ OpenBSD (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/latest/download/redis-backup_openbsd_amd64 -o /usr/local/bin/redis-backup
chmod +x /usr/local/bin/redis-backup
```

**✅ FreeBSD (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/latest/download/redis-backup_freebsd_amd64 -o /usr/local/bin/redis-backup
chmod +x /usr/local/bin/redis-backup
```

---

## ✅ Usage Examples

**Basic backup:**

```bash
sudo redis-backup
```

**Limit local to 1 copy, keep 4× more on FTP:**

```bash
sudo redis-backup --copies 1 --ftp-keep-factor 4
```

**Check freshness (24h) + disk usage + FTP:**

```bash
redis-backup --check 24 --copies 2 --ftp-keep-factor 4
```

---

## 📁 Backup Locations

| Where | How Many                       |
| ----- | ------------------------------ |
| Local | `--copies`                     |
| FTP   | `--copies × --ftp-keep-factor` |

---

## 🔄 Restore

```bash
sudo redis-backup --restore
```

* Pick Redis port.
* Pick archive.
* The current `RDB` is renamed to `.backup` and replaced safely.

---

## 🔍 Nagios Command Example

```bash
/usr/local/bin/check_ssh r2d2@$HOSTADDRESS$ sudo /usr/local/bin/redis-backup --check 24
```

---

# 🇷🇺 Русская инструкция

---

## 📦 Обзор

**redis-backup** — это удобный инструмент на Go, который:

* Автоматически находит все работающие Redis.
* Сохраняет их `RDB` в виде `.tar.gz` архивов.
* Отправляет архивы сразу на несколько FTP серверов.
* Гибко управляет количеством копий локально и на FTP.
* Позволяет интерактивно восстановить данные.
* Проверяет свежесть, размер, FTP и диск.
* Ставит лок-файл для защиты от параллельного запуска.
* Выводит цветные логи.

---

## ✅ Новые возможности

* 🔗 **Мульти-FTP** — сколько угодно серверов для надёжности.
* ⏳ **Ограничение локальных копий** (`--copies`) и длинная история на FTP (`--ftp-keep-factor`).
* 🕵️ **Режим проверки (`--check`)** — следит за всем.
* 🔄 **Безопасное восстановление**.

---

## ⚙️ Новые флаги

| Флаг                | Описание                                                    | По умолчанию |
| ------------------- | ----------------------------------------------------------- | ------------ |
| `--copies`, `-c`    | Сколько daily-файлов хранить локально (0 = без ограничения) | `0`          |
| `--ftp-keep-factor` | Во сколько раз дольше хранить на FTP                        | `4`          |

---

## 🗂️ Пример /etc/ftp-backup.conf

```ini
FTP_HOST=ftp1.example.com
FTP_USER=user1
FTP_PASS=pass1

FTP_HOST=ftp2.backup.net
FTP_USER=user2
FTP_PASS=pass2
```

---

## 🚀 Установка

**✅ Linux (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/latest/download/redis-backup_linux_amd64 -o /usr/local/bin/redis-backup
chmod +x /usr/local/bin/redis-backup
```

**✅ macOS (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/latest/download/redis-backup_darwin_amd64 -o /usr/local/bin/redis-backup
chmod +x /usr/local/bin/redis-backup
```

**✅ OpenBSD (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/latest/download/redis-backup_openbsd_amd64 -o /usr/local/bin/redis-backup
chmod +x /usr/local/bin/redis-backup
```

**✅ FreeBSD (amd64)**

```bash
curl -L https://github.com/matveynator/redis-backup/releases/latest/download/redis-backup_freebsd_amd64 -o /usr/local/bin/redis-backup
chmod +x /usr/local/bin/redis-backup
```

---

## 📋 Примеры использования

**Обычный бэкап:**

```bash
sudo redis-backup
```

**Локально — 1 копия, на FTP — 4 раза больше:**

```bash
sudo redis-backup --copies 1 --ftp-keep-factor 4
```

**Проверка свежести (24ч), места и FTP:**

```bash
redis-backup --check 24 --copies 2 --ftp-keep-factor 4
```

---

## 📁 Где хранятся бэкапы

| Где      | Сколько копий                  |
| -------- | ------------------------------ |
| Локально | `--copies`                     |
| На FTP   | `--copies × --ftp-keep-factor` |

---

## 🔄 Восстановление

```bash
sudo redis-backup --restore
```

* Выбрать порт Redis.
* Выбрать архив.
* Текущий RDB переименуется в `.backup` и заменится.

---

## 🔍 Пример команды для Nagios

```bash
/usr/local/bin/check_ssh r2d2@$HOSTADDRESS$ sudo /usr/local/bin/redis-backup --check 24
```

---

## 🧹 Автоудаление

* Локально — удаляются лишние daily-архивы по `--copies`.
* На FTP — аналогично, но копий хранится `× --ftp-keep-factor`.

---

## 📑 License

GNU GPL.

