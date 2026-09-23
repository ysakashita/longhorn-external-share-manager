#!/bin/sh
set -eu

: "${SMB_SHARE_PATH:?SMB_SHARE_PATH is required}"
SMB_SHARE_NAME="${SMB_SHARE_NAME:-share}"

mkdir -p /etc/samba /var/lib/samba/private /var/run/samba /var/log/samba

if [ -n "${SMB_USER:-}" ] && [ -n "${SMB_PASSWORD:-}" ]; then
	# Authenticated mode: create the Samba user and require valid credentials.
	id "$SMB_USER" >/dev/null 2>&1 || adduser -D -H -s /sbin/nologin "$SMB_USER"
	printf '%s\n%s\n' "$SMB_PASSWORD" "$SMB_PASSWORD" | smbpasswd -s -a "$SMB_USER"

	cat >/etc/samba/smb.conf <<EOF
[global]
    workgroup = WORKGROUP
    server string = Longhorn External Share Manager SMB Gateway
    security = user
    map to guest = never
    server min protocol = SMB2
    log level = 1

[${SMB_SHARE_NAME}]
    path = ${SMB_SHARE_PATH}
    browsable = yes
    read only = no
    guest ok = no
    valid users = ${SMB_USER}
    force user = ${SMB_USER}
EOF
else
	# Default mode: no authentication (guest access). This mirrors the
	# project's existing stance that the plain NFS export has no access
	# control -- the SMB gateway's default posture matches it.
	cat >/etc/samba/smb.conf <<EOF
[global]
    workgroup = WORKGROUP
    server string = Longhorn External Share Manager SMB Gateway
    security = user
    map to guest = bad user
    guest account = nobody
    server min protocol = SMB2
    log level = 1

[${SMB_SHARE_NAME}]
    path = ${SMB_SHARE_PATH}
    browsable = yes
    read only = no
    guest ok = yes
    force user = nobody
EOF
fi

exec smbd --foreground --no-process-group
