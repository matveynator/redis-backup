# SFTP backup targets

`redis-backup` can replicate every local Redis archive to one or more SFTP servers in addition to FTP.

The implementation uses the system OpenSSH `sftp` client. This keeps host-key verification and SSH key handling in the normal OpenSSH configuration and does not put passwords on the command line.

## Configuration

Default configuration file: `/etc/sftp-backup.conf`

```ini
SFTP_HOST=backup1.example.com
SFTP_PORT=22
SFTP_USER=backup01
SFTP_KEY=/root/.ssh/id_ed25519_backup
SFTP_ROOT=/backup

SFTP_HOST=backup2.example.com
SFTP_PORT=2222
SFTP_USER=backup02
SFTP_KEY=/root/.ssh/id_ed25519_backup2
SFTP_KNOWN_HOSTS=/root/.ssh/known_hosts
SFTP_ROOT=/data
```

Each `SFTP_HOST` starts a new target. `SFTP_PORT` defaults to `22` and `SFTP_ROOT` defaults to `/`.

`SFTP_KEY` is optional. When omitted, OpenSSH uses its normal SSH agent, default keys and `~/.ssh/config` rules.

Host key verification is always enabled. If `SFTP_KNOWN_HOSTS` is not specified, OpenSSH uses its normal `known_hosts` configuration.

## Command-line configuration

A single target can also be supplied with flags:

```text
--sftp-host backup.example.com
--sftp-port 22
--sftp-user backup01
--sftp-key /root/.ssh/id_ed25519_backup
--sftp-root /backup
--sftp-known-hosts /root/.ssh/known_hosts
--sftp-keep-factor 4
```

## Retention

SFTP uses the same policy as FTP:

- with `--copies N`, SFTP keeps `N × --sftp-keep-factor` daily archives;
- without `--copies`, SFTP retention is `--days × --sftp-keep-factor` days.

Archives are uploaded as `*.part` first and renamed only after a successful transfer, so monitoring does not treat incomplete uploads as valid backups.

## Monitoring

`--check <hours>` checks every configured SFTP host and Redis port. It reports CRITICAL when the remote backup is missing, too old, or the SFTP connection/list operation fails. It reports WARNING when fewer retained copies exist than requested.

The remote freshness check uses the timestamp embedded in the archive filename and does not download or decompress remote backups.

## Requirements

The `sftp` executable from OpenSSH must be installed and usable non-interactively with SSH keys or an SSH agent.
