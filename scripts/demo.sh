#!/usr/bin/env bash
# Сквозной сценарий через curl: права, изоляция команд, конфликт версий,
# курсорная пагинация, снятие исполнителя, отчёт и ограничение частоты.
# Каждый ответ сверяется с ожидаемым кодом; при расхождении скрипт завершается
# с ненулевым кодом, поэтому его же запускает CI.
#
# Часть 1 читает засеянные данные (docker compose --profile demo up), если они
# есть. Часть 2 создаёт собственных пользователей и команду, поэтому скрипт можно
# запускать повторно. Бюджет лимита: до 10 регистраций и до 10 входов в минуту
# с одного IP, сценарий укладывается в 4 и 7. Если вход упёрся в лимит, скрипт
# ждёт Retry-After и повторяет. Зависимости: bash, curl, sed.
#
# Переменные: API (по умолчанию http://127.0.0.1:${APP_PORT:-18080}/api/v1),
# DEMO_PASSWORD (пароль демо-пользователей), NO_COLOR.

set -u

API=${API:-http://127.0.0.1:${APP_PORT:-18080}/api/v1}
DEMO_PASSWORD=${DEMO_PASSWORD:-demo-password}
PASSWORD='correct-horse-battery'

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
PASSED=0
FAILED=0
STEP=0

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    BOLD=$'\033[1m'; GREEN=$'\033[32m'; RED=$'\033[31m'; DIM=$'\033[2m'; RESET=$'\033[0m'
else
    BOLD=''; GREEN=''; RED=''; DIM=''; RESET=''
fi

step() {
    STEP=$((STEP + 1))
    printf '\n%s%d. %s%s\n' "$BOLD" "$STEP" "$1" "$RESET"
}

note() {
    printf '   %s%s%s\n' "$DIM" "$1" "$RESET"
}

summarize() {
    if printf '%s' "$RESPONSE" | grep -q '"error"'; then
        printf '%s' "$RESPONSE" | sed -n 's/.*"code":"\([^"]*\)","message":"\([^"]*\)".*/\1: \2/p'
    else
        printf '%s' "$RESPONSE" | cut -c1-210
    fi
}

call() {
    local label=$1 method=$2 path=$3 token=$4 body=$5 expected=$6
    local args=(-s -o "$WORK/body" -D "$WORK/headers" -w '%{http_code}' -X "$method" "$API$path")
    [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
    [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")

    STATUS=$(curl "${args[@]}")
    RESPONSE=$(cat "$WORK/body")

    if [ "$STATUS" = "$expected" ]; then
        PASSED=$((PASSED + 1))
        printf '   %s✓%s %s -> %s  %s%s%s\n' "$GREEN" "$RESET" "$label" "$STATUS" "$DIM" "$(summarize)" "$RESET"
    else
        FAILED=$((FAILED + 1))
        printf '   %s✗%s %s -> %s, ожидался %s\n     %s\n' "$RED" "$RESET" "$label" "$STATUS" "$expected" "$(summarize)"
    fi
}

json_number() {
    printf '%s' "$RESPONSE" | sed -n "s/.*\"$1\":\([0-9][0-9]*\).*/\1/p" | head -n 1
}

first_id() {
    printf '%s' "$RESPONSE" | sed -n 's/^{"id":\([0-9][0-9]*\),.*/\1/p'
}

login() {
    local email=$1 password=$2 attempt wait_seconds
    TOKEN=''
    for attempt in 1 2; do
        STATUS=$(curl -s -o "$WORK/body" -D "$WORK/headers" -w '%{http_code}' -X POST "$API/login" \
            -H 'Content-Type: application/json' -d "{\"email\":\"$email\",\"password\":\"$password\"}")
        if [ "$STATUS" != "429" ]; then
            break
        fi
        wait_seconds=$(sed -n 's/^[Rr]etry-[Aa]fter: *\([0-9][0-9]*\).*/\1/p' "$WORK/headers" | head -n 1)
        note "лимит входов, жду ${wait_seconds:-5} с"
        sleep "${wait_seconds:-5}"
    done
    TOKEN=$(sed -n 's/.*"token":"\([^"]*\)".*/\1/p' "$WORK/body")
}

team_id_by_name() {
    printf '%s' "$RESPONSE" | grep -o "\"id\":[0-9]*,\"name\":\"$1\"" | head -n 1 | sed 's/"id":\([0-9]*\),.*/\1/'
}

printf '%sTask Service: демонстрация%s\nAPI: %s\n' "$BOLD" "$RESET" "$API"

if ! curl -s -f -o /dev/null "${API%/api/v1}/healthz"; then
    printf '%sСервис не отвечает.%s Запустите его: docker compose --profile demo up --build -d\n' "$RED" "$RESET"
    exit 1
fi

login demo-owner@example.com "$DEMO_PASSWORD"; OWNER=$TOKEN
if [ -n "$OWNER" ]; then
    login demo-member@example.com "$DEMO_PASSWORD"; MEMBER=$TOKEN
    login demo-outsider@example.com "$DEMO_PASSWORD"; OUTSIDER=$TOKEN

    step "Демо-данные: роли пользователя в разных командах"
    call "Вера (участник «Разработки», администратор «Маркетинга»): GET /teams" GET /teams "$MEMBER" "" 200
    DEV=$(team_id_by_name 'Разработка')
    MKT=$(team_id_by_name 'Маркетинг')

    step "Изоляция команд"
    call "Егор не состоит ни в одной команде: GET /teams" GET /teams "$OUTSIDER" "" 200
    call "Егор запрашивает задачи чужой команды" GET "/tasks?team_id=$DEV" "$OUTSIDER" "" 404
    call "Егор запрашивает отчёт чужой команды" GET "/teams/$DEV/stats" "$OUTSIDER" "" 404
    call "Вера читает задачи «Разработки»" GET "/tasks?team_id=$DEV&limit=2" "$MEMBER" "" 200

    step "Отчёт по команде: только владелец и администратор"
    call "Анна (владелец) смотрит отчёт «Разработки»" GET "/teams/$DEV/stats" "$OWNER" "" 200
    note "топ-3 за 30 дней: Вера 3, Глеб 2, Борис 1; «Настроить CI» закрыта 35 дней назад и не входит"
    call "Вера (обычный участник «Разработки») просит тот же отчёт" GET "/teams/$DEV/stats" "$MEMBER" "" 403
    call "Вера (администратор «Маркетинга») смотрит отчёт «Маркетинга»" GET "/teams/$MKT/stats" "$MEMBER" "" 200
else
    printf '\n%sДемо-данные не загружены, часть 1 пропущена.%s Чтобы загрузить: docker compose --profile demo up --build -d\n' "$DIM" "$RESET"
fi

RUN="$(date +%s)$RANDOM"
register() {
    call "регистрация $1" POST /register "" "{\"email\":\"$1-$RUN@demo.test\",\"password\":\"$PASSWORD\",\"name\":\"$2\"}" 201
    REGISTERED_ID=$(json_number id)
}

step "Новые пользователи: руководитель, исполнитель, наблюдатель и посторонний"
register lead 'Руководитель'; LEAD_ID=$REGISTERED_ID
register dev 'Исполнитель'; DEV_ID=$REGISTERED_ID
register guest 'Наблюдатель'; GUEST_ID=$REGISTERED_ID
register stranger 'Посторонний'; STRANGER_ID=$REGISTERED_ID
login "lead-$RUN@demo.test" "$PASSWORD"; LEAD=$TOKEN
login "dev-$RUN@demo.test" "$PASSWORD"; DEVELOPER=$TOKEN
login "guest-$RUN@demo.test" "$PASSWORD"; GUEST=$TOKEN
login "stranger-$RUN@demo.test" "$PASSWORD"; STRANGER=$TOKEN

step "Команда и приглашения по email"
call "руководитель создаёт команду" POST /teams "$LEAD" "{\"name\":\"Демо $RUN\"}" 201
TEAM=$(first_id)
call "приглашает исполнителя" POST "/teams/$TEAM/invite" "$LEAD" "{\"email\":\"dev-$RUN@demo.test\",\"role\":\"member\"}" 201
call "приглашает наблюдателя" POST "/teams/$TEAM/invite" "$LEAD" "{\"email\":\"guest-$RUN@demo.test\",\"role\":\"member\"}" 201
call "наблюдатель пытается приглашать" POST "/teams/$TEAM/invite" "$GUEST" "{\"email\":\"stranger-$RUN@demo.test\"}" 403
call "роль owner приглашением не выдаётся" POST "/teams/$TEAM/invite" "$LEAD" "{\"email\":\"stranger-$RUN@demo.test\",\"role\":\"owner\"}" 400

step "Задачи и права на поля"
call "создание задачи с исполнителем" POST /tasks "$LEAD" "{\"team_id\":$TEAM,\"title\":\"Подготовить релиз\",\"description\":\"Собрать changelog\",\"assignee_id\":$DEV_ID}" 201
TASK=$(first_id)
call "исполнитель из другой команды запрещён" POST /tasks "$LEAD" "{\"team_id\":$TEAM,\"title\":\"Чужой исполнитель\",\"assignee_id\":$STRANGER_ID}" 400
call "посторонний создаёт задачу в чужой команде" POST /tasks "$STRANGER" "{\"team_id\":$TEAM,\"title\":\"Вторжение\"}" 404
UPDATE_FIELDS="\"description\":\"Собрать changelog\",\"assignee_id\":$DEV_ID"
call "наблюдатель (не автор и не исполнитель) меняет статус" PUT "/tasks/$TASK" "$GUEST" "{\"title\":\"Подготовить релиз\",$UPDATE_FIELDS,\"status\":\"in_progress\",\"version\":1}" 403
call "исполнитель меняет только статус" PUT "/tasks/$TASK" "$DEVELOPER" "{\"title\":\"Подготовить релиз\",$UPDATE_FIELDS,\"status\":\"in_progress\",\"version\":1}" 200
call "исполнитель пытается переименовать" PUT "/tasks/$TASK" "$DEVELOPER" "{\"title\":\"Новое название\",$UPDATE_FIELDS,\"status\":\"in_progress\",\"version\":2}" 403
call "исполнитель пытается переназначить задачу" PUT "/tasks/$TASK" "$DEVELOPER" "{\"title\":\"Подготовить релиз\",\"description\":\"Собрать changelog\",\"assignee_id\":$GUEST_ID,\"status\":\"in_progress\",\"version\":2}" 403
call "посторонний читает историю чужой задачи" GET "/tasks/$TASK/history" "$STRANGER" "" 404

step "Конкурентное обновление: два PUT с одной версией"
note "версия задачи сейчас 2; оба запроса отправляются одновременно"
for who in A B; do
    curl -s -o "$WORK/race-$who.body" -w '%{http_code}\n' -X PUT "$API/tasks/$TASK" \
        -H "Authorization: Bearer $LEAD" -H 'Content-Type: application/json' \
        -d "{\"title\":\"Правка $who\",$UPDATE_FIELDS,\"status\":\"in_progress\",\"version\":2}" > "$WORK/race-$who.status" &
done
wait
RACE=$(cat "$WORK/race-A.status" "$WORK/race-B.status" | sort | tr '\n' ' ')
CONFLICT=$(cat "$WORK/race-A.body" "$WORK/race-B.body" | sed -n 's/.*"current_version":\([0-9][0-9]*\).*/\1/p')
if [ "$RACE" = "200 409 " ] && [ "$CONFLICT" = "3" ]; then
    PASSED=$((PASSED + 1)); printf '   %s✓%s коды: %s, проигравший получил current_version=%s\n' "$GREEN" "$RESET" "$RACE" "$CONFLICT"
else
    FAILED=$((FAILED + 1)); printf '   %s✗%s ожидалось «200 409» и current_version=3, получено: %s, %s\n' "$RED" "$RESET" "$RACE" "$CONFLICT"
fi
call "история: создание, смена статуса и одна из двух правок" GET "/tasks/$TASK/history" "$LEAD" "" 200
HISTORY_TOTAL=$(json_number total)
if [ "$HISTORY_TOTAL" = "3" ]; then
    PASSED=$((PASSED + 1)); printf '   %s✓%s в истории ровно 3 записи, проигравший запрос ничего не записал\n' "$GREEN" "$RESET"
else
    FAILED=$((FAILED + 1)); printf '   %s✗%s записей в истории %s, ожидалось 3\n' "$RED" "$RESET" "$HISTORY_TOTAL"
fi

step "Курсорная пагинация списка"
call "ещё две задачи" POST /tasks "$LEAD" "{\"team_id\":$TEAM,\"title\":\"Вторая\"}" 201
call "ещё одна" POST /tasks "$LEAD" "{\"team_id\":$TEAM,\"title\":\"Третья\"}" 201
call "страница 1 (limit=2)" GET "/tasks?team_id=$TEAM&limit=2" "$GUEST" "" 200
CURSOR=$(json_number next_cursor)
note "next_cursor = $CURSOR, id последней задачи страницы"
call "страница 2 по курсору" GET "/tasks?team_id=$TEAM&limit=2&cursor=$CURSOR" "$GUEST" "" 200
call "cursor вместе с offset запрещён" GET "/tasks?team_id=$TEAM&cursor=$CURSOR&offset=1" "$GUEST" "" 400

step "Комментарии и отчёт"
call "наблюдатель комментирует задачу" POST "/tasks/$TASK/comments" "$GUEST" '{"content":"Проверил, всё в порядке"}' 201
call "руководитель смотрит отчёт" GET "/teams/$TEAM/stats" "$LEAD" "" 200
call "наблюдатель просит отчёт" GET "/teams/$TEAM/stats" "$GUEST" "" 403

step "Удаление участника снимает его с задач"
call "руководитель удаляет исполнителя" DELETE "/teams/$TEAM/members/$DEV_ID" "$LEAD" "" 204
call "задача осталась без исполнителя" GET "/tasks?team_id=$TEAM&limit=100" "$LEAD" "" 200
if printf '%s' "$RESPONSE" | grep -q "\"id\":$TASK,\"team_id\":$TEAM,\"title\":\"[^\"]*\",\"description\":\"Собрать changelog\",\"status\":\"in_progress\",\"created_by\":$LEAD_ID,\"assignee_id\":null"; then
    PASSED=$((PASSED + 1)); printf '   %s✓%s у задачи %s assignee_id = null\n' "$GREEN" "$RESET" "$TASK"
else
    FAILED=$((FAILED + 1)); printf '   %s✗%s задача %s всё ещё с исполнителем\n' "$RED" "$RESET" "$TASK"
fi
call "бывший исполнитель потерял доступ" GET "/tasks?team_id=$TEAM" "$DEVELOPER" "" 404
call "руководитель не может удалить самого себя" DELETE "/teams/$TEAM/members/$LEAD_ID" "$LEAD" "" 403

step "Ограничение частоты: 12 неверных входов подряд с одного IP"
note "лимит входа по умолчанию 10 в минуту; после него сервис отвечает 429 с Retry-After"
LIMITED=0; ALLOWED=0
for attempt in 1 2 3 4 5 6 7 8 9 10 11 12; do
    code=$(curl -s -o /dev/null -D "$WORK/limit-headers" -w '%{http_code}' -X POST "$API/login" \
        -H 'Content-Type: application/json' -d '{"email":"nobody@demo.test","password":"wrong-password"}')
    if [ "$code" = "429" ]; then LIMITED=$((LIMITED + 1)); else ALLOWED=$((ALLOWED + 1)); fi
done
RETRY=$(sed -n 's/^[Rr]etry-[Aa]fter: *\([0-9][0-9]*\).*/\1/p' "$WORK/limit-headers" | head -n 1)
if [ "$LIMITED" -ge 1 ] && [ -n "$RETRY" ]; then
    PASSED=$((PASSED + 1)); printf '   %s✓%s пропущено %s, отклонено %s, Retry-After: %s с\n' "$GREEN" "$RESET" "$ALLOWED" "$LIMITED" "$RETRY"
else
    FAILED=$((FAILED + 1)); printf '   %s✗%s лимит не сработал: пропущено %s, отклонено %s\n' "$RED" "$RESET" "$ALLOWED" "$LIMITED"
fi

printf '\n%sИтог:%s %s%d проверок пройдено%s' "$BOLD" "$RESET" "$GREEN" "$PASSED" "$RESET"
if [ "$FAILED" -gt 0 ]; then
    printf ', %s%d не пройдено%s\n' "$RED" "$FAILED" "$RESET"
    exit 1
fi
printf '\n'
