#!/bin/sh
# Заливает демонстрационные данные из seed.sql прямо в базу, минуя API.
# Запускается одноразовой службой seed из профиля demo и только после того, как
# приложение стало healthy, то есть после применения миграций. Повторный запуск
# ничего не делает: наличие демо-пользователя считается признаком загруженных данных.

set -eu

run() {
    MYSQL_PWD="$MYSQL_PASSWORD" mysql --default-character-set=utf8mb4 -h "$MYSQL_HOST" -u "$MYSQL_USER" "$MYSQL_DATABASE" "$@"
}

loaded=$(run -N -e "SELECT COUNT(*) FROM users WHERE email = 'demo-owner@example.com'")
if [ "$loaded" != "0" ]; then
    echo "demo data is already loaded, nothing to do"
    exit 0
fi

run < /demo/seed.sql
echo "demo data loaded"
