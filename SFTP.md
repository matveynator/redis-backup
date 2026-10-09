# SFTP backups

`redis-backup` can replicate every Redis archive to one or more SFTP servers in addition to FTP.

The SFTP backend uses the system OpenSSH `sftp` client. Authentication therefore uses normal SSH keys, `ssh-agent` and `~/.ssh/config`; passwords are not put on the command line.

## Where the settings are

The default configuration file is:

```text
/etc/sftp-backup.conf
```

Example:

```ini
SFTP_HOST=static.213-133-127-244.clients.your-server.de
SFTP_PORT=22
SFTP_USER=backup08
SFTP_KEY=/root/.ssh/id_ed25519
SFTP_ROOT=auto
```

`SFTP_ROOT=auto` is the recommended/default mode.

Each new `SFTP_HOST` starts another target, so one config file can contain multiple independent SFTP backup servers.

```ini
SFTP_HOST=backup1.example.com
SFTP_USER=backup01
SFTP_KEY=/root/.ssh/backup01
SFTP_ROOT=auto

SFTP_HOST=backup2.example.com
SFTP_PORT=2222
SFTP_USER=backup02
SFTP_KEY=/root/.ssh/backup02
SFTP_ROOT=/data
```

## Automatic writable-directory detection

Before the first archive upload, `redis-backup` connects to every configured SFTP target and performs a very small write test. It creates and immediately removes a temporary directory. Large Redis archives are not uploaded until this test succeeds.

With `SFTP_ROOT=auto`, the following locations are tried in order:

```text
.
/data
/backup
/backups
/upload
/uploads
/home/<SFTP_USER>
```

The first writable location becomes the SFTP root for the rest of that run.

Example log:

```text
SFTP probing backup.example.com:22 (user backup08) for a writable directory
SFTP replication target -> backup.example.com:22 (user backup08), writable root /data
```

This is useful for chrooted `internal-sftp` accounts where `/` is intentionally owned by `root:root` and cannot be written by the backup user, while a directory such as `/data` is writable.

If no candidate is writable, that SFTP target is disabled for the run before any multi-gigabyte archive upload is attempted. `--check` reports the target as CRITICAL.

If the connection itself is reset or the SFTP subsystem cannot start, candidate probing stops immediately because changing the directory cannot fix a transport/server-side failure. The error includes host, port and user for diagnosis.

## Explicit SFTP root

To force one location instead of auto-detection:

```ini
SFTP_ROOT=/data
```

Only `/data` is tested. If it is not writable, the target is disabled for that run.

For a chroot where the SFTP login starts directly inside a writable directory, you can use:

```ini
SFTP_ROOT=.
```

Relative paths stay relative; `redis-backup` does not incorrectly turn them into `/redis01/...` absolute paths.

## Host key handling

The client uses:

```text
StrictHostKeyChecking=accept-new
```

A previously unseen host key is accepted and written to `known_hosts` automatically. A changed key is still rejected, protecting against unexpected host-key replacement.

To use a dedicated known-hosts file:

```ini
SFTP_KNOWN_HOSTS=/root/.ssh/known_hosts
```

## SSH authentication

Recommended key-based setup:

```ini
SFTP_USER=backup08
SFTP_KEY=/root/.ssh/id_ed25519
```

`SFTP_KEY` is optional. If omitted, OpenSSH uses the normal SSH agent, default identity files and SSH config rules.

Connections are non-interactive (`BatchMode=yes`) and use connection/keepalive timeouts suitable for unattended backups.

## Command-line override

A single target can be supplied directly:

```text
--sftp-host backup.example.com
--sftp-port 22
--sftp-user backup08
--sftp-key /root/.ssh/id_ed25519
--sftp-root auto
--sftp-known-hosts /root/.ssh/known_hosts
--sftp-keep-factor 4
```

When `--sftp-host` is specified, it replaces all targets from `/etc/sftp-backup.conf`. It does not append to the configured list.

## Remote path layout

After the writable root is selected, the normal backup structure is preserved. For example, if `/data` is selected:

```text
/data/redis01/redis-backup/redis_6385/daily/2026-10-09_03-50-23_redis_6385.tar.gz
```

If `.` is selected:

```text
redis01/redis-backup/redis_6385/daily/2026-10-09_03-50-23_redis_6385.tar.gz
```

Archives are uploaded as `*.part` and renamed only after a successful transfer. Incomplete uploads are therefore not treated as valid backups.

## Retention

SFTP uses the same retention model as FTP:

- with `--copies N`, SFTP keeps `N × --sftp-keep-factor` daily archives;
- without `--copies`, SFTP retention is `--days × --sftp-keep-factor` days.

The default SFTP retention multiplier is `4`.

## Monitoring

`--check <hours>` checks every configured SFTP host and Redis port.

It reports CRITICAL when:

- the SFTP target cannot initialize;
- no writable root can be found;
- the connection/subsystem fails;
- the remote backup is missing;
- the newest backup is older than the requested threshold.

It reports WARNING when fewer retained copies exist than requested.

The remote freshness check uses the timestamp in the archive filename and does not download or decompress the remote backup.

Example:

```bash
redis-backup --check 24 --copies 2 --sftp-keep-factor 4
```

## Troubleshooting `Connection reset by peer`

If auto-detection reports a transport reset such as:

```text
Couldn't read packet: Connection reset by peer
```

this normally happens before directory permissions can be tested. Check the SFTP server's SSH logs and chroot configuration, especially `internal-sftp`, `ChrootDirectory`, ownership/modes and whether the requested SFTP subsystem is allowed.

Typical server-side log commands on Debian/Ubuntu are:

```bash
journalctl -u ssh -n 100 --no-pager
```

or:

```bash
tail -100 /var/log/auth.log
```

For OpenSSH chroot setups, the chroot directory itself normally must be owned by `root` and not writable by the SFTP user. Put writable storage below it, for example:

```text
/backup/backup08       root:root
/backup/backup08/data  backup08:backup08
```

Then `SFTP_ROOT=auto` should discover `/data` automatically.

## Requirements

The OpenSSH `sftp` executable must be installed and usable non-interactively with SSH keys or an SSH agent.
