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
# Режим --record готовит прогон для записи экрана: паузы между шагами и
# проверками, короткие строки шириной до 100 колонок (вместо JSON краткий итог)
# и дополнительный шаг «Отказ Redis», который останавливает контейнер Redis
# командой docker compose. Поэтому режим запускается при стеке, поднятом через
# compose. Redis возвращается всегда: при нормальном завершении, при ошибке, при
# Ctrl+C и при SIGTERM. Возврат проверяется: прерывание в момент, когда команда
# остановки уже ушла демону, оставило бы Redis остановленным, поэтому запуск
# повторяется, пока контейнер не будет работать два раза подряд. Не помогает
# только SIGKILL; тогда выполните docker compose start redis.
#
# Переменные: API (по умолчанию http://127.0.0.1:${APP_PORT:-18080}/api/v1),
# DEMO_PASSWORD (пароль демо-пользователей), NO_COLOR, а для --record ещё
# DEMO_COLUMNS (ширина, 100), DEMO_STEP_PAUSE (1.5 с), DEMO_CHECK_PAUSE (0.7 с)
# и DEMO_KEY_PAUSE (пауза на главных кадрах, 2.5 с).

set -u

RECORD=0
for argument in "$@"; do
    case "$argument" in
        --record) RECORD=1 ;;
        -h | --help)
            echo "usage: scripts/demo.sh [--record]"
            exit 0
            ;;
        *)
            echo "неизвестный аргумент: $argument (допустимо: --record)" >&2
            exit 2
            ;;
    esac
done

API=${API:-http://127.0.0.1:${APP_PORT:-18080}/api/v1}
DEMO_PASSWORD=${DEMO_PASSWORD:-demo-password}
PASSWORD='correct-horse-battery'
WIDTH=${DEMO_COLUMNS:-100}
STEP_PAUSE=${DEMO_STEP_PAUSE:-1.5}
CHECK_PAUSE=${DEMO_CHECK_PAUSE:-0.7}
KEY_PAUSE=${DEMO_KEY_PAUSE:-2.5}

WORK=$(mktemp -d)
REDIS_STOPPED=0
PROJECT_DIR=$(cd "$(dirname "$0")/.." && pwd)
PASSED=0
FAILED=0
STEP=0
SHOW_TIME=0
LAST_MS=0

redis_running() {
    [ -n "$(cd "$PROJECT_DIR" && docker compose ps -q --status running redis 2>/dev/null)" ]
}

restore_redis() {
    local attempt confirmed=0
    if [ "$REDIS_STOPPED" != 1 ]; then
        return
    fi
    for attempt in 1 2 3 4 5 6; do
        (cd "$PROJECT_DIR" && docker compose start redis >/dev/null 2>&1)
        sleep 1.5
        if redis_running; then
            confirmed=$((confirmed + 1))
        else
            confirmed=0
        fi
        if [ "$confirmed" -ge 2 ]; then
            REDIS_STOPPED=0
            printf '\nRedis возвращён (docker compose start redis)\n' >&2
            return
        fi
    done
    printf '\nRedis вернуть не удалось, выполните: docker compose start redis\n' >&2
}

cleanup() {
    restore_redis
    rm -rf "$WORK"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [ "$RECORD" = 1 ]; then
    case "$(locale charmap 2>/dev/null)" in
        UTF-8) ;;
        *) export LC_ALL=C.UTF-8 ;;
    esac
fi

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    BOLD=$'\033[1m'; GREEN=$'\033[32m'; RED=$'\033[31m'; DIM=$'\033[2m'; RESET=$'\033[0m'
else
    BOLD=''; GREEN=''; RED=''; DIM=''; RESET=''
fi

pause() {
    if [ "$RECORD" = 1 ]; then
        sleep "$1"
    fi
}

hold() {
    pause "$KEY_PAUSE"
}

fit() {
    local text=$1 limit=$2
    if [ "${#text}" -gt "$limit" ]; then
        printf '%s…' "${text:0:$((limit - 1))}"
    else
        printf '%s' "$text"
    fi
}

step() {
    if [ "$STEP" -gt 0 ]; then
        pause "$STEP_PAUSE"
    fi
    STEP=$((STEP + 1))
    printf '\n%s%d. %s%s\n' "$BOLD" "$STEP" "$(fit "$1" $((WIDTH - 5)))" "$RESET"
}

note() {
    printf '   %s%s%s\n' "$DIM" "$(fit "$1" $((WIDTH - 4)))" "$RESET"
}

json_text() {
    printf '%s' "$RESPONSE" | sed -n "s/.*\"$1\":\"\([^\"]*\)\".*/\1/p" | head -n 1
}

json_number() {
    printf '%s' "$RESPONSE" | sed -n "s/.*\"$1\":\([0-9][0-9]*\).*/\1/p" | head -n 1
}

first_id() {
    printf '%s' "$RESPONSE" | sed -n 's/^{"id":\([0-9][0-9]*\),.*/\1/p'
}

error_text() {
    printf '%s' "$RESPONSE" | sed -n 's/.*"code":"\([^"]*\)","message":"\([^"]*\)".*/\1: \2/p'
}

summarize() {
    if printf '%s' "$RESPONSE" | grep -q '"error"'; then
        error_text
    else
        printf '%s' "$RESPONSE" | cut -c1-210
    fi
}

brief() {
    local pairs top
    case "$RESPONSE" in
        *'"error"'*) error_text ;;
        '['*)
            pairs=$(printf '%s' "$RESPONSE" | grep -o '"name":"[^"]*","created_by":[0-9]*,"created_at":"[^"]*","role":"[a-z]*"' \
                | sed 's/"name":"\([^"]*\)".*"role":"\([^"]*\)"/\1 (\2)/' | paste -sd, - | sed 's/,/, /g')
            printf 'команды: %s' "${pairs:-нет}"
            ;;
        *'"tasks_by_status"'*)
            top=$(printf '%s' "$RESPONSE" | grep -o '"name":"[^"]*","closed_tasks":[0-9]*' \
                | sed 's/"name":"\([^" ]*\)[^"]*","closed_tasks":\([0-9]*\)/\1 \2/' | paste -sd, - | sed 's/,/, /g')
            printf 'todo %s, in_progress %s, done %s | топ: %s | комментариев: %s' \
                "$(json_number todo)" "$(json_number in_progress)" "$(json_number done)" "${top:-пусто}" "$(json_number comments_total)"
            ;;
        *'"next_cursor"'*)
            printf 'на странице: %s, всего: %s, next_cursor: %s' \
                "$(printf '%s' "$RESPONSE" | grep -o '"team_id":' | wc -l | tr -d ' ')" "$(json_number total)" \
                "$(printf '%s' "$RESPONSE" | sed -n 's/.*"next_cursor":\([0-9a-z]*\).*/\1/p')"
            ;;
        *'"items":['*) printf 'записей: %s' "$(json_number total)" ;;
        *'"token"'*) printf 'токен получен' ;;
        *'"content"'*) printf 'комментарий №%s: %s' "$(json_number id)" "$(json_text content)" ;;
        *'"title"'*) printf 'задача №%s «%s», %s, версия %s' "$(first_id)" "$(json_text title)" "$(json_text status)" "$(json_number version)" ;;
        *'"email"'*) printf 'пользователь №%s' "$(json_number id)" ;;
        *'"created_by"'*) printf 'команда №%s «%s», ваша роль: %s' "$(first_id)" "$(json_text name)" "$(json_text role)" ;;
        *'"user_id"'*) printf 'участник №%s, роль: %s' "$(json_number user_id)" "$(json_text role)" ;;
        *) printf '%s' "$RESPONSE" ;;
    esac
}

report() {
    local mark=$1 label=$2 detail=$3 timing=''
    if [ "$SHOW_TIME" = 1 ]; then
        timing=" ($LAST_MS мс)"
    fi
    if [ "$RECORD" = 1 ]; then
        printf '   %s %s%s  %s%s\n' "$mark" "$STATUS" "$RESET" "$(fit "$label" $((WIDTH - 10 - ${#timing})))" "$timing"
        if [ -n "$detail" ]; then
            printf '       %s↳ %s%s\n' "$DIM" "$(fit "$detail" $((WIDTH - 9)))" "$RESET"
        fi
    else
        printf '   %s %s -> %s%s  %s%s%s\n' "$mark" "$label" "$STATUS" "$timing" "$DIM" "$detail" "$RESET"
    fi
}

call() {
    local label=$1 method=$2 path=$3 token=$4 body=$5 expected=$6 result
    local args=(-s -o "$WORK/body" -D "$WORK/headers" -w '%{http_code} %{time_total}' -X "$method" "$API$path")
    [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
    [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")

    result=$(curl "${args[@]}")
    STATUS=${result%% *}
    LAST_MS=$(printf '%s' "${result#* }" | awk '{printf "%d", $1 * 1000}')
    RESPONSE=$(cat "$WORK/body")

    if [ "$STATUS" = "$expected" ]; then
        PASSED=$((PASSED + 1))
        if [ "$RECORD" = 1 ]; then
            report "${GREEN}✓" "$label" "$(brief)"
        else
            report "${GREEN}✓${RESET}" "$label" "$(summarize)"
        fi
    else
        FAILED=$((FAILED + 1))
        if [ "$RECORD" = 1 ]; then
            report "${RED}✗" "$label (ожидался $expected)" "$(brief)"
        else
            printf '   %s✗%s %s -> %s, ожидался %s\n     %s\n' "$RED" "$RESET" "$label" "$STATUS" "$expected" "$(summarize)"
        fi
    fi
    pause "$CHECK_PAUSE"
}

verdict() {
    local ok=$1 good=$2 bad=$3
    if [ "$ok" = 1 ]; then
        PASSED=$((PASSED + 1))
        printf '   %s✓%s %s\n' "$GREEN" "$RESET" "$(fit "$good" $((WIDTH - 6)))"
    else
        FAILED=$((FAILED + 1))
        printf '   %s✗%s %s\n' "$RED" "$RESET" "$(fit "$bad" $((WIDTH - 6)))"
    fi
    pause "$CHECK_PAUSE"
}

extra() {
    if [ "$RECORD" = 0 ]; then
        call "$@"
    fi
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

rate_limit_step() {
    local attempt code limited=0 allowed=0 retry
    for attempt in 1 2 3 4 5 6 7 8 9 10 11 12; do
        code=$(curl -s -o /dev/null -D "$WORK/limit-headers" -w '%{http_code}' -X POST "$API/login" \
            -H 'Content-Type: application/json' -d '{"email":"nobody@demo.test","password":"wrong-password"}')
        if [ "$code" = "429" ]; then limited=$((limited + 1)); else allowed=$((allowed + 1)); fi
    done
    retry=$(sed -n 's/^[Rr]etry-[Aa]fter: *\([0-9][0-9]*\).*/\1/p' "$WORK/limit-headers" | head -n 1)
    if [ "$limited" -ge 1 ] && [ -n "$retry" ]; then
        verdict 1 "пропущено $allowed, отклонено $limited (429), Retry-After: $retry с" ""
        hold
    else
        verdict 0 "" "лимит не сработал: пропущено $allowed, отклонено $limited"
    fi
}

redis_outage_step() {
    local list="/tasks?team_id=$TEAM&limit=3" since attempt opened closed=0
    step "Отказ Redis: сервис продолжает работать"
    call "список задач, Redis работает" GET "$list" "$GUEST" "" 200
    note "\$ docker compose stop redis"
    since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    REDIS_STOPPED=1
    (cd "$PROJECT_DIR" && docker compose stop redis >/dev/null 2>&1)
    hold
    SHOW_TIME=1
    for attempt in 1 2 3 4 5 6; do
        call "список задач без Redis, запрос $attempt" GET "$list" "$GUEST" "" 200
    done
    SHOW_TIME=0
    call "вход с неверным паролем: лимит пропускает, а не блокирует" POST /login "" \
        '{"email":"nobody@demo.test","password":"wrong-password"}' 401
    call "задача меняется, пока Redis недоступен" PUT "/tasks/$THIRD" "$LEAD" \
        '{"title":"Третья: изменена без Redis","description":"","status":"todo","assignee_id":null,"version":1}' 200
    call "список без Redis сразу показывает изменение" GET "$list" "$GUEST" "" 200
    verdict "$(printf '%s' "$RESPONSE" | grep -q 'изменена без Redis' && echo 1 || echo 0)" \
        "в списке новое название задачи" "в списке старое название задачи"
    hold
    note "\$ docker compose start redis"
    (cd "$PROJECT_DIR" && docker compose start redis >/dev/null 2>&1)
    REDIS_STOPPED=0
    opened=$(cd "$PROJECT_DIR" && docker compose logs --no-log-prefix app --since "$since" 2>/dev/null | grep -c '"to":"open"')
    if [ "$opened" -gt 0 ]; then
        note "жду, пока предохранитель замкнётся (пауза 5 с и проба)"
        for attempt in $(seq 1 30); do
            closed=$(cd "$PROJECT_DIR" && docker compose logs --no-log-prefix app --since "$since" 2>/dev/null | grep -c '"to":"closed"')
            [ "$closed" -gt 0 ] && break
            curl -s -o /dev/null -H "Authorization: Bearer $GUEST" "$API$list"
            sleep 1
        done
        verdict "$([ "$closed" -gt 0 ] && echo 1 || echo 0)" \
            "предохранитель замкнулся: closed → open → half-open → closed" "предохранитель не замкнулся за 30 с"
        hold
    else
        verdict 0 "" "предохранитель не разомкнулся: сбои Redis не были замечены"
    fi
    SHOW_TIME=1
    call "список после возвращения Redis: свежий" GET "$list" "$GUEST" "" 200
    SHOW_TIME=0
    verdict "$(printf '%s' "$RESPONSE" | grep -q 'изменена без Redis' && echo 1 || echo 0)" \
        "изменение не потерялось" "изменение потерялось"
    hold
}

printf '%sTask Service: демонстрация%s\nAPI: %s\n' "$BOLD" "$RESET" "$API"

if ! curl -s -f -o /dev/null "${API%/api/v1}/healthz"; then
    printf '%sСервис не отвечает.%s Запустите его: docker compose --profile demo up --build -d\n' "$RED" "$RESET"
    exit 1
fi

if [ "$RECORD" = 1 ]; then
    if ! (cd "$PROJECT_DIR" && docker compose ps -q redis 2>/dev/null | grep -q .); then
        printf '%sРежим --record не нашёл контейнер Redis.%s Поднимите стек: docker compose --profile demo up --build -d\n' "$RED" "$RESET"
        exit 1
    fi
    note "режим записи: паузы, короткие строки, шаг «Отказ Redis»"
fi

login demo-owner@example.com "$DEMO_PASSWORD"; OWNER=$TOKEN
if [ -n "$OWNER" ]; then
    login demo-member@example.com "$DEMO_PASSWORD"; MEMBER=$TOKEN
    login demo-outsider@example.com "$DEMO_PASSWORD"; OUTSIDER=$TOKEN

    step "Демо-данные: роли пользователя в разных командах"
    call "Вера: участник «Разработки», администратор «Маркетинга»" GET /teams "$MEMBER" "" 200
    DEV=$(team_id_by_name 'Разработка')
    MKT=$(team_id_by_name 'Маркетинг')

    step "Изоляция команд"
    call "Егор не состоит ни в одной команде" GET /teams "$OUTSIDER" "" 200
    call "Егор запрашивает задачи чужой команды" GET "/tasks?team_id=$DEV" "$OUTSIDER" "" 404
    extra "Егор запрашивает отчёт чужой команды" GET "/teams/$DEV/stats" "$OUTSIDER" "" 404
    extra "Вера читает задачи «Разработки»" GET "/tasks?team_id=$DEV&limit=2" "$MEMBER" "" 200

    step "Отчёт по команде: только владелец и администратор"
    call "Анна (владелец) смотрит отчёт «Разработки»" GET "/teams/$DEV/stats" "$OWNER" "" 200
    note "топ-3 за 30 дней: Вера 3, Глеб 2, Борис 1; «Настроить CI» закрыта 35 дней назад и не входит"
    hold
    call "Вера (обычный участник «Разработки») просит тот же отчёт" GET "/teams/$DEV/stats" "$MEMBER" "" 403
    hold
    extra "Вера (администратор «Маркетинга») смотрит отчёт «Маркетинга»" GET "/teams/$MKT/stats" "$MEMBER" "" 200
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
extra "роль owner приглашением не выдаётся" POST "/teams/$TEAM/invite" "$LEAD" "{\"email\":\"stranger-$RUN@demo.test\",\"role\":\"owner\"}" 400

step "Задачи и права на поля"
call "создание задачи с исполнителем" POST /tasks "$LEAD" "{\"team_id\":$TEAM,\"title\":\"Подготовить релиз\",\"description\":\"Собрать changelog\",\"assignee_id\":$DEV_ID}" 201
TASK=$(first_id)
call "исполнитель из другой команды запрещён" POST /tasks "$LEAD" "{\"team_id\":$TEAM,\"title\":\"Чужой исполнитель\",\"assignee_id\":$STRANGER_ID}" 400
call "посторонний создаёт задачу в чужой команде" POST /tasks "$STRANGER" "{\"team_id\":$TEAM,\"title\":\"Вторжение\"}" 404
UPDATE_FIELDS="\"description\":\"Собрать changelog\",\"assignee_id\":$DEV_ID"
call "наблюдатель (не автор и не исполнитель) меняет статус" PUT "/tasks/$TASK" "$GUEST" "{\"title\":\"Подготовить релиз\",$UPDATE_FIELDS,\"status\":\"in_progress\",\"version\":1}" 403
call "исполнитель меняет только статус" PUT "/tasks/$TASK" "$DEVELOPER" "{\"title\":\"Подготовить релиз\",$UPDATE_FIELDS,\"status\":\"in_progress\",\"version\":1}" 200
call "исполнитель пытается переименовать" PUT "/tasks/$TASK" "$DEVELOPER" "{\"title\":\"Новое название\",$UPDATE_FIELDS,\"status\":\"in_progress\",\"version\":2}" 403
extra "исполнитель пытается переназначить задачу" PUT "/tasks/$TASK" "$DEVELOPER" "{\"title\":\"Подготовить релиз\",\"description\":\"Собрать changelog\",\"assignee_id\":$GUEST_ID,\"status\":\"in_progress\",\"version\":2}" 403
extra "посторонний читает историю чужой задачи" GET "/tasks/$TASK/history" "$STRANGER" "" 404

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
    verdict 1 "коды: 200 и 409, проигравший получил current_version=$CONFLICT" ""
    hold
else
    verdict 0 "" "ожидалось «200 409» и current_version=3, получено: $RACE, $CONFLICT"
fi
call "история: создание, смена статуса и одна из двух правок" GET "/tasks/$TASK/history" "$LEAD" "" 200
HISTORY_TOTAL=$(json_number total)
if [ "$HISTORY_TOTAL" = "3" ]; then
    verdict 1 "в истории ровно 3 записи, проигравший запрос ничего не записал" ""
else
    verdict 0 "" "записей в истории $HISTORY_TOTAL, ожидалось 3"
fi

step "Курсорная пагинация списка"
call "ещё две задачи" POST /tasks "$LEAD" "{\"team_id\":$TEAM,\"title\":\"Вторая\"}" 201
call "ещё одна" POST /tasks "$LEAD" "{\"team_id\":$TEAM,\"title\":\"Третья\"}" 201
THIRD=$(first_id)
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
    verdict 1 "у задачи $TASK assignee_id = null" ""
else
    verdict 0 "" "задача $TASK всё ещё с исполнителем"
fi
call "бывший исполнитель потерял доступ" GET "/tasks?team_id=$TEAM" "$DEVELOPER" "" 404
extra "руководитель не может удалить самого себя" DELETE "/teams/$TEAM/members/$LEAD_ID" "$LEAD" "" 403

if [ "$RECORD" = 1 ]; then
    redis_outage_step
    step "Лимит снова работает: 12 неверных входов подряд"
    note "Redis вернулся с пустыми счётчиками, лимит входа 10 в минуту"
    rate_limit_step
else
    step "Ограничение частоты: 12 неверных входов подряд с одного IP"
    note "лимит входа по умолчанию 10 в минуту; после него сервис отвечает 429 с Retry-After"
    rate_limit_step
fi

printf '\n%sИтог:%s %s%d проверок пройдено%s' "$BOLD" "$RESET" "$GREEN" "$PASSED" "$RESET"
if [ "$FAILED" -gt 0 ]; then
    printf ', %s%d не пройдено%s\n' "$RED" "$FAILED" "$RESET"
    exit 1
fi
printf '\n'
