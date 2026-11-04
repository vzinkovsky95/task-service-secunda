#!/bin/sh
# Создаёт .env из .env.example и подставляет случайные пароли и секрет JWT.
# Работает на Linux и macOS: только POSIX sh, od, tr и mv, без sed -i.
# Существующий .env не перезаписывается без флага --force.

set -eu

cd "$(dirname "$0")/.."

force=0
case "${1:-}" in
    --force) force=1 ;;
    "") ;;
    *) echo "usage: scripts/init-env.sh [--force]" >&2; exit 2 ;;
esac

if [ -e .env ] && [ "$force" -eq 0 ]; then
    echo ".env already exists, leaving it untouched (use --force to regenerate)" >&2
    exit 0
fi

random_hex() {
    od -An -N"$1" -tx1 /dev/urandom | tr -d ' \n'
}

mysql_password=$(random_hex 12)
mysql_root_password=$(random_hex 12)
jwt_secret=$(random_hex 32)

umask 077
tmp=$(mktemp .env.XXXXXX)
trap 'rm -f "$tmp"' EXIT

while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
        MYSQL_PASSWORD=*) printf 'MYSQL_PASSWORD=%s\n' "$mysql_password" ;;
        MYSQL_ROOT_PASSWORD=*) printf 'MYSQL_ROOT_PASSWORD=%s\n' "$mysql_root_password" ;;
        JWT_SECRET=*) printf 'JWT_SECRET=%s\n' "$jwt_secret" ;;
        *) printf '%s\n' "$line" ;;
    esac
done < .env.example > "$tmp"

mv "$tmp" .env
trap - EXIT
echo ".env created with generated MYSQL_PASSWORD, MYSQL_ROOT_PASSWORD and JWT_SECRET"
