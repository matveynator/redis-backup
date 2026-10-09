# SFTP backups

`redis-backup` can replicate Redis archives to one or more SFTP servers in addition to FTP.

SFTP is implemented directly in Go. The server does **not** need the `ssh` or `sftp` command-line clients installed. `redis-backup` opens SSH/SFTP connections itself.

## Configuration

Default configuration file:

```text
/etc/sftp-backup.conf
```

Recommended example:

```ini
SFTP_HOST=static.213-133-127-244.clients.your-server.de
SFTP_PORT=22
SFTP_USER=backup12
SFTP_KEY=/root/.ssh/id_ed25519
SFTP_ROOT=auto
```

Multiple `SFTP_HOST` sections can be placed in the same file.

## Authentication

`SFTP_KEY` may point to an SSH private key:

```ini
SFTP_KEY=/root/.ssh/id_ed25519
```

If it is omitted, `redis-backup` tries the SSH agent and standard keys in `~/.ssh/` (`id_ed25519`, `id_ecdsa`, `id_rsa`).

Host keys are accepted silently. The native Go SSH client does not prompt for `known_hosts` confirmation. `SFTP_KNOWN_HOSTS` is retained only for configuration compatibility and is ignored by the native backend.

## Automatic writable-directory discovery

`SFTP_ROOT=auto` is the default and recommended mode.

The program does **not** guess directory names such as `/data`, `/backup` or `/uploads`.

It does this instead:

1. Opens one native SSH/SFTP session.
2. Tries a tiny create/remove write probe in the current SFTP directory (`.`).
3. If `.` is not writable, lists the actual visible entries with the SFTP API (`ReadDir(".")`).
4. Keeps only entries that really exist and are directories.
5. Tries the same tiny write probe inside each visible directory.
6. Uses the first writable directory as the SFTP root for the current run.

This works well with chrooted SFTP accounts where the chroot root is intentionally read-only but one of its child directories is writable.

Example layout:

```text
/backup/backup12/        root:root
/backup/backup12/data/   backup12:backup12
```

The SFTP user sees `data` after login. `redis-backup` discovers it from the server and selects it automatically.

If the connection, authentication or SFTP subsystem fails, discovery stops immediately; it does not repeat connection attempts for guessed paths.

## Explicit root

You can force a path:

```ini
SFTP_ROOT=/data/backups
```

For an explicit root, `redis-backup` creates the directory hierarchy with the native SFTP API before testing write access. This preserves first-run behavior when the destination does not exist yet.

For a login that starts directly inside a writable chroot directory:

```ini
SFTP_ROOT=.
```

Relative paths remain relative.

## Remote path layout

If the selected root is `data`:

```text
data/redis01/redis-backup/redis_6385/daily/2026-10-09_03-50-23_redis_6385.tar.gz
```

If the selected root is `.`:

```text
redis01/redis-backup/redis_6385/daily/2026-10-09_03-50-23_redis_6385.tar.gz
```

Uploads are written as `*.part` first and renamed only after a complete transfer.

## Command-line override

```text
--sftp-host backup.example.com
--sftp-port 22
--sftp-user backup12
--sftp-key /root/.ssh/id_ed25519
--sftp-root auto
--sftp-keep-factor 4
```

When `--sftp-host` is supplied, targets from `/etc/sftp-backup.conf` are replaced rather than appended.

## Retention

With `--copies N`, SFTP keeps:

```text
N × --sftp-keep-factor
```

daily archives.

Without `--copies`, the retention period is `--days × --sftp-keep-factor`.

## Monitoring

`--check <hours>` checks each configured SFTP target and Redis port using the native SFTP client. One SFTP session is reused for all Redis ports on the same target during a check.

CRITICAL is reported when the target cannot initialize, authentication/SFTP fails, no writable root is found, a backup is missing, or the newest backup is too old.

Example:

```bash
redis-backup --check 24 --copies 2 --sftp-keep-factor 4
```

Remote archives are listed through SFTP; they are not downloaded or decompressed during monitoring.

## Requirements

No external SSH/SFTP client is required. Only network access to the SSH/SFTP server and a usable SSH private key or SSH agent are needed.
