#!/usr/bin/env bash
#
# cli_test.sh — end-to-end smoke test for the notifyctl CLI.
#
# Boots the full stack (Postgres + NATS via docker compose, then the gRPC
# server) and drives every major `notifyctl` command against it, asserting on
# real output. The dead-letter queue (DLQ) lifecycle — list and replay — is
# exercised explicitly, since that is the most operationally important path.
#
# Because a genuine DLQ entry only appears after a delivery exhausts its retry
# budget (5 attempts with exponential backoff ≈ 30s+, and flaky), we force the
# failure the same way the e2e harness does: send a notification to an
# unreachable webhook, then flip its job row to FAILED/retry_count=10 in
# Postgres so the very next scheduler poll dead-letters it. This is
# deterministic and fast, and still drives the real ListDeadLetters /
# ReplayDeadLetter RPCs through the CLI.
#
# Usage:
#   scripts/cli_test.sh                # bring up infra, run, tear down
#   KEEP_INFRA=1 scripts/cli_test.sh   # leave docker compose + server running
#   NO_COMPOSE=1 scripts/cli_test.sh   # assume Postgres/NATS already running
#
# Requirements: docker (with compose plugin), go toolchain, psql client.
#
set -uo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

PG_USER="${POSTGRES_USER:-notifyctl}"
PG_PASS="${POSTGRES_PASSWORD:-your_secure_password_here}"
PG_DB="${POSTGRES_DB:-notifyctl}"
PG_HOST="${PG_HOST:-localhost}"
# Host ports for the docker compose stack. Defaults are deliberately
# non-standard (matching the e2e harness) so we don't collide with a Postgres
# or NATS the developer already runs on the usual 5432/4222. Override with
# PG_PORT / NATS_PORT if needed; ignored entirely under NO_COMPOSE.
PG_PORT="${PG_PORT:-55432}"
NATS_PORT="${NATS_PORT:-54222}"

DATABASE_URL="${DATABASE_URL:-postgres://${PG_USER}:${PG_PASS}@${PG_HOST}:${PG_PORT}/${PG_DB}?sslmode=disable}"
NATS_URL="${NATS_URL:-nats://localhost:${NATS_PORT}}"
SERVER_ADDR="${SERVER_ADDR:-localhost:50051}"

BIN_DIR="$REPO_ROOT/bin"
NOTIFYCTL="$BIN_DIR/notifyctl"
SERVER_BIN="$BIN_DIR/server"

WORK_DIR="$(mktemp -d)"
SERVER_LOG="$WORK_DIR/server.log"
SERVER_PID=""
COMPOSE_OVERRIDE="$WORK_DIR/compose.override.yml"

# psql connection string (kept separate so we can run raw SQL for DLQ injection).
PSQL_DSN="postgresql://${PG_USER}:${PG_PASS}@${PG_HOST}:${PG_PORT}/${PG_DB}"

# ---------------------------------------------------------------------------
# Pretty output + assertion helpers
# ---------------------------------------------------------------------------
if [[ -t 1 ]]; then
  C_GREEN=$'\033[32m'; C_RED=$'\033[31m'; C_YEL=$'\033[33m'; C_BLU=$'\033[36m'; C_RST=$'\033[0m'
else
  C_GREEN=""; C_RED=""; C_YEL=""; C_BLU=""; C_RST=""
fi

PASS_COUNT=0
FAIL_COUNT=0
CURRENT_SECTION=""

section() { CURRENT_SECTION="$1"; printf '\n%s━━ %s %s\n' "$C_BLU" "$1" "$C_RST"; }
info()    { printf '   %s· %s%s\n' "$C_YEL" "$1" "$C_RST"; }

pass() { PASS_COUNT=$((PASS_COUNT+1)); printf '   %s✓%s %s\n' "$C_GREEN" "$C_RST" "$1"; }
fail() {
  FAIL_COUNT=$((FAIL_COUNT+1))
  printf '   %s✗%s %s\n' "$C_RED" "$C_RST" "$1"
  [[ -n "${2:-}" ]] && printf '       %s\n' "$2"
}

# assert_contains <description> <haystack> <needle>
assert_contains() {
  if [[ "$2" == *"$3"* ]]; then pass "$1"; else
    fail "$1" "expected to find: '$3'"
    printf '       got: %s\n' "$(printf '%s' "$2" | head -c 400 | tr '\n' ' ')"
  fi
}

# assert_not_contains <description> <haystack> <needle>
assert_not_contains() {
  if [[ "$2" != *"$3"* ]]; then pass "$1"; else
    fail "$1" "did NOT expect: '$3'"
  fi
}

# assert_nonempty <description> <value>
assert_nonempty() {
  if [[ -n "${2// /}" ]]; then pass "$1"; else fail "$1" "value was empty"; fi
}

# assert_eq <description> <actual> <expected>
assert_eq() {
  if [[ "$2" == "$3" ]]; then pass "$1"; else
    fail "$1" "expected '$3' but got '$2'"
  fi
}

die() { printf '%s\nFATAL: %s%s\n' "$C_RED" "$1" "$C_RST" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Infra lifecycle
# ---------------------------------------------------------------------------
DOCKER_COMPOSE=""
detect_compose() {
  if docker compose version >/dev/null 2>&1; then DOCKER_COMPOSE="docker compose";
  elif command -v docker-compose >/dev/null 2>&1; then DOCKER_COMPOSE="docker-compose";
  else die "docker compose not found"; fi
}

cleanup() {
  local code=$?
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    info "stopping server (pid $SERVER_PID)"
    kill "$SERVER_PID" 2>/dev/null
    wait "$SERVER_PID" 2>/dev/null
  fi
  if [[ -z "${KEEP_INFRA:-}" && -z "${NO_COMPOSE:-}" ]]; then
    info "tearing down docker compose"
    if [[ -f "$COMPOSE_OVERRIDE" ]]; then
      $DOCKER_COMPOSE -f docker-compose.yml -f "$COMPOSE_OVERRIDE" down -v >/dev/null 2>&1
    else
      $DOCKER_COMPOSE down -v >/dev/null 2>&1
    fi
  fi
  if [[ -n "${KEEP_INFRA:-}" ]]; then
    info "KEEP_INFRA set — leaving stack up. Server log: $SERVER_LOG"
  else
    rm -rf "$WORK_DIR"
  fi
  exit "$code"
}
trap cleanup EXIT INT TERM

wait_for_postgres() {
  info "waiting for postgres at ${PG_HOST}:${PG_PORT}"
  for _ in $(seq 1 30); do
    if PGPASSWORD="$PG_PASS" psql "$PSQL_DSN" -c 'SELECT 1' >/dev/null 2>&1; then
      pass "postgres is ready"; return 0
    fi
    sleep 1
  done
  die "postgres did not become ready in time"
}

wait_for_server() {
  info "waiting for gRPC server at ${SERVER_ADDR}"
  local host="${SERVER_ADDR%%:*}" port="${SERVER_ADDR##*:}"
  for _ in $(seq 1 30); do
    if (exec 3<>"/dev/tcp/${host}/${port}") 2>/dev/null; then
      exec 3>&- 3<&-
      pass "server is accepting connections"; return 0
    fi
    # Surface an early crash instead of waiting the full window.
    if [[ -n "$SERVER_PID" ]] && ! kill -0 "$SERVER_PID" 2>/dev/null; then
      printf '%s\n' "--- server log ---" >&2; cat "$SERVER_LOG" >&2
      die "server process exited during startup"
    fi
    sleep 1
  done
  printf '%s\n' "--- server log ---" >&2; cat "$SERVER_LOG" >&2
  die "server did not become ready in time"
}

# psql_exec <sql>  — run a statement against the server's database.
psql_exec() { PGPASSWORD="$PG_PASS" psql "$PSQL_DSN" -tAc "$1"; }

# ---------------------------------------------------------------------------
# Setup
# ---------------------------------------------------------------------------
command -v go   >/dev/null || die "go toolchain not found"
command -v psql >/dev/null || die "psql client not found"
detect_compose

section "Build binaries"
info "go build ./cmd/server and ./cmd/notifyctl"
go build -o "$SERVER_BIN"    ./cmd/server    || die "server build failed"
go build -o "$NOTIFYCTL"     ./cmd/notifyctl || die "cli build failed"
pass "binaries built"

if [[ -z "${NO_COMPOSE:-}" ]]; then
  section "Start infrastructure (Postgres + NATS)"
  # Remap container ports to our (non-standard) host ports so the stack never
  # collides with a Postgres/NATS already bound to 5432/4222 on this host.
  # `!override` (Compose v2.24+) replaces the base ports list instead of
  # appending to it — otherwise the base 5432/4222 mappings would still be
  # published and collide.
  cat >"$COMPOSE_OVERRIDE" <<EOF
services:
  postgres:
    ports: !override
      - "${PG_PORT}:5432"
  nats:
    ports: !override
      - "${NATS_PORT}:4222"
      - "58222:8222"
EOF
  info "host ports: postgres=${PG_PORT}, nats=${NATS_PORT}"
  POSTGRES_USER="$PG_USER" POSTGRES_PASSWORD="$PG_PASS" POSTGRES_DB="$PG_DB" \
    $DOCKER_COMPOSE -f docker-compose.yml -f "$COMPOSE_OVERRIDE" up -d >/dev/null 2>&1 \
    || die "docker compose up failed (is ${PG_PORT} or ${NATS_PORT} already in use?)"
  pass "docker compose started"
else
  section "Using pre-existing infrastructure (NO_COMPOSE=1)"
fi
wait_for_postgres

section "Start notifyctl server"
DATABASE_URL="$DATABASE_URL" NATS_URL="$NATS_URL" "$SERVER_BIN" >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!
info "server pid $SERVER_PID, log: $SERVER_LOG"
wait_for_server

# Common flags: point the CLI at our server and keep timeouts tight.
NC=("$NOTIFYCTL" --timeout 15s)

# ===========================================================================
# 1. service create / list / delete
# ===========================================================================
section "service create"
SVC_NAME="cli-test-$$"
# Unreachable webhook target so deliveries fail and feed the DLQ later.
WEBHOOK_URL="http://127.0.0.1:1/unreachable"
CREATE_JSON="$("${NC[@]}" --json service create --name "$SVC_NAME" --webhook-url "$WEBHOOK_URL" --secret "s3cret" 2>&1)"
assert_contains "create returns service_id" "$CREATE_JSON" "service_id"
assert_contains "create returns api_key"    "$CREATE_JSON" "api_key"

# Extract serviceId + apiKey from the JSON (python3 if present, else sed fallback).
extract_json() { # <json> <key>
  if command -v python3 >/dev/null 2>&1; then
    printf '%s' "$1" | python3 -c "import sys,json;print(json.load(sys.stdin).get('$2',''))" 2>/dev/null
  else
    printf '%s' "$1" | sed -n "s/.*\"$2\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" | head -1
  fi
}
SERVICE_ID="$(extract_json "$CREATE_JSON" service_id)"
API_KEY="$(extract_json "$CREATE_JSON" api_key)"
assert_nonempty "parsed serviceId" "$SERVICE_ID"
assert_nonempty "parsed apiKey"    "$API_KEY"
info "serviceId=$SERVICE_ID"

# Configure the CLI's credentials via env for the rest of the run.
export NOTIFYCTL_SERVICE_ID="$SERVICE_ID"
export NOTIFYCTL_API_KEY="$API_KEY"

section "service list"
LIST_OUT="$("${NC[@]}" --json service list 2>&1)"
assert_contains "list includes our service id" "$LIST_OUT" "$SERVICE_ID"

# ===========================================================================
# 2. send
# ===========================================================================
section "send (--raw)"
PAYLOAD_RAW="$(cat <<EOF
{"topic":"order.created","payload":{"order_id":"A-1"},"destinations":[{"type":1,"target":"$WEBHOOK_URL"}]}
EOF
)"
SEND_OUT="$("${NC[@]}" --json --service-id "$SERVICE_ID" --auth-token "$API_KEY" send --raw "$PAYLOAD_RAW" 2>&1)"
assert_contains "send returns notification_id" "$SEND_OUT" "notification_id"
NOTIF_ID="$(extract_json "$SEND_OUT" notification_id)"
assert_nonempty "parsed notificationId" "$NOTIF_ID"
info "notificationId=$NOTIF_ID"

section "send (--payload file)"
PAYLOAD_FILE="$WORK_DIR/payload.json"
cat >"$PAYLOAD_FILE" <<EOF
{"topic":"order.shipped","payload":{"order_id":"A-2"},"destinations":[{"type":1,"target":"$WEBHOOK_URL"}]}
EOF
SEND2_OUT="$("${NC[@]}" --json --service-id "$SERVICE_ID" --auth-token "$API_KEY" send --payload "$PAYLOAD_FILE" 2>&1)"
NOTIF_ID2="$(extract_json "$SEND2_OUT" notification_id)"
assert_nonempty "send from file returns notificationId" "$NOTIF_ID2"

section "send rejects missing payload"
ERR_OUT="$("${NC[@]}" --service-id "$SERVICE_ID" --auth-token "$API_KEY" send 2>&1)"
assert_contains "errors when neither --raw nor --payload given" "$ERR_OUT" "must provide either"

# ===========================================================================
# 3. logs (jobs + stats)
# ===========================================================================
section "logs (notification jobs)"
LOGS_OUT="$("${NC[@]}" --service-id "$SERVICE_ID" --auth-token "$API_KEY" logs 2>&1)"
assert_contains "logs lists our notification" "$LOGS_OUT" "$NOTIF_ID"

section "logs --stats"
STATS_OUT="$("${NC[@]}" --json --service-id "$SERVICE_ID" --auth-token "$API_KEY" logs --stats 2>&1)"
assert_nonempty "stats returns output" "$STATS_OUT"

# ===========================================================================
# 4. DLQ lifecycle — the headline feature
# ===========================================================================
# Force the first notification past its retry budget so the scheduler
# dead-letters it on the next poll, the same technique the e2e harness uses.
#
# A live worker is also processing this job, so we must let its delivery
# attempt settle first — otherwise its async status write races our forced
# UPDATE and flips the row back. We wait until the job leaves PENDING, then
# pin it to FAILED/retry_count=10 and confirm the write stuck.
section "DLQ: force dead-letter for $NOTIF_ID"
info "waiting for the worker's delivery attempt to settle"
for _ in $(seq 1 20); do
  ST="$(psql_exec "SELECT status FROM notification_jobs WHERE request_id='$NOTIF_ID'")"
  [[ -n "$ST" && "$ST" != "PENDING" && "$ST" != "ACCEPTED" && "$ST" != "DISPATCHED" ]] && break
  sleep 1
done
info "job settled at status=${ST:-?}; forcing terminal failure"
# Pin to FAILED past the retry budget. Retry once if a late worker write races us.
for _ in $(seq 1 5); do
  psql_exec "UPDATE notification_jobs SET status='FAILED', retry_count=10, next_retry_at=NOW() WHERE request_id='$NOTIF_ID'" >/dev/null
  CONFIRM="$(psql_exec "SELECT status||':'||retry_count FROM notification_jobs WHERE request_id='$NOTIF_ID'")"
  [[ "$CONFIRM" == "FAILED:10" ]] && break
  sleep 1
done
assert_eq "job row pinned to FAILED/retry_count=10" "$CONFIRM" "FAILED:10"

info "waiting for scheduler to dead-letter the job (poll interval 5s)"
DEAD_LETTERED=""
for _ in $(seq 1 20); do
  STATUS="$(psql_exec "SELECT status FROM notification_jobs WHERE request_id='$NOTIF_ID'")"
  DL_COUNT="$(psql_exec "SELECT count(*) FROM dead_letters WHERE notification_id='$NOTIF_ID'")"
  if [[ "$STATUS" == "DEAD_LETTERED" && "$DL_COUNT" == "1" ]]; then
    DEAD_LETTERED=1; break
  fi
  sleep 1
done
if [[ -n "$DEAD_LETTERED" ]]; then
  pass "job transitioned to DEAD_LETTERED with a dead_letters row"
else
  fail "job was not dead-lettered in time (status=$STATUS, dl_count=$DL_COUNT)"
fi

section "dlq list"
DLQ_JSON="$("${NC[@]}" --json --service-id "$SERVICE_ID" --auth-token "$API_KEY" dlq list 2>&1)"
assert_contains "dlq list (json) references the dead-lettered notification" "$DLQ_JSON" "$NOTIF_ID"

# Table form should carry the expected header columns.
DLQ_TABLE="$("${NC[@]}" --service-id "$SERVICE_ID" --auth-token "$API_KEY" dlq list 2>&1)"
assert_contains "dlq list (table) has NOTIFICATION ID column" "$DLQ_TABLE" "NOTIFICATION ID"
assert_contains "dlq list (table) has LAST ERROR column"      "$DLQ_TABLE" "LAST ERROR"

# Quiet form: just dead-letter IDs, one per line. Grab the first as our target.
DLQ_IDS="$("${NC[@]}" -q --service-id "$SERVICE_ID" --auth-token "$API_KEY" dlq list 2>&1)"
DL_ID="$(printf '%s\n' "$DLQ_IDS" | head -1)"
assert_nonempty "dlq list -q yields a dead-letter id" "$DL_ID"
info "dead-letter id=$DL_ID"

section "dlq list scoping (PermissionDenied for another service)"
OTHER_OUT="$("${NC[@]}" --service-id "$SERVICE_ID" --auth-token "$API_KEY" dlq list --service-id "00000000-0000-0000-0000-000000000000" 2>&1)"
assert_contains "cross-service dlq list is rejected" "$OTHER_OUT" "another service"

section "dlq replay <id>"
REPLAY_OUT="$("${NC[@]}" --service-id "$SERVICE_ID" --auth-token "$API_KEY" dlq replay "$DL_ID" 2>&1)"
assert_contains "replay confirms re-enqueue" "$REPLAY_OUT" "re-enqueued notification"
assert_contains "replay references original notification id" "$REPLAY_OUT" "$NOTIF_ID"

info "verifying replay reset the job and removed the dead-letter record"
REPLAYED=""
for _ in $(seq 1 15); do
  DL_AFTER="$(psql_exec "SELECT count(*) FROM dead_letters WHERE id='$DL_ID'")"
  STATUS_AFTER="$(psql_exec "SELECT status FROM notification_jobs WHERE request_id='$NOTIF_ID'")"
  # After replay the job is re-queued (PENDING) and may immediately move on;
  # the durable signal is that the dead_letters row is gone.
  if [[ "$DL_AFTER" == "0" ]]; then REPLAYED=1; break; fi
  sleep 1
done
if [[ -n "$REPLAYED" ]]; then
  pass "dead-letter record removed after replay (job status now: ${STATUS_AFTER:-?})"
else
  fail "dead-letter record still present after replay"
fi

section "dlq list empty after replay"
DLQ_AFTER="$("${NC[@]}" -q --service-id "$SERVICE_ID" --auth-token "$API_KEY" dlq list 2>&1)"
assert_not_contains "replayed dead-letter id no longer listed" "$DLQ_AFTER" "$DL_ID"

section "dlq replay unknown id errors"
BOGUS_OUT="$("${NC[@]}" --service-id "$SERVICE_ID" --auth-token "$API_KEY" dlq replay "00000000-0000-0000-0000-000000000000" 2>&1)"
assert_contains "replaying a non-existent dead-letter errors" "$BOGUS_OUT" "Error"

# ===========================================================================
# 5. auth handling
# ===========================================================================
section "auth: missing API key is rejected"
( unset NOTIFYCTL_API_KEY NOTIFYCTL_SERVICE_ID
  NOAUTH_OUT="$("$NOTIFYCTL" --timeout 10s dlq list 2>&1)"
  assert_contains "dlq without API key is rejected" "$NOAUTH_OUT" "missing API key"
)

# ===========================================================================
# 6. service delete (cleanup of the test service)
# ===========================================================================
section "service delete"
# Delete operates on a fresh, data-free service: notification_jobs / stats /
# dead_letters all FK-reference services(id) without ON DELETE CASCADE, so a
# service that has sent traffic cannot be deleted. This isolates the delete RPC.
DEL_CREATE="$("${NC[@]}" --json service create --name "del-$$" --webhook-url "$WEBHOOK_URL" 2>&1)"
DEL_SVC_ID="$(extract_json "$DEL_CREATE" service_id)"
DEL_API_KEY="$(extract_json "$DEL_CREATE" api_key)"
assert_nonempty "throwaway service created for delete test" "$DEL_SVC_ID"

DEL_OUT="$("${NC[@]}" -q --service-id "$DEL_SVC_ID" --auth-token "$DEL_API_KEY" service delete --id "$DEL_SVC_ID" 2>&1)"
DEL_STATUS=$?
assert_eq "service delete exits 0" "$DEL_STATUS" "0"
LIST_AFTER="$("${NC[@]}" --json --auth-token "$DEL_API_KEY" service list 2>&1)"
assert_not_contains "deleted service no longer listed" "$LIST_AFTER" "$DEL_SVC_ID"

# ===========================================================================
# Summary
# ===========================================================================
printf '\n%s━━ Summary %s\n' "$C_BLU" "$C_RST"
printf '   %s%d passed%s' "$C_GREEN" "$PASS_COUNT" "$C_RST"
if [[ "$FAIL_COUNT" -gt 0 ]]; then
  printf ', %s%d failed%s\n' "$C_RED" "$FAIL_COUNT" "$C_RST"
  exit 1
fi
printf '\n%s   ALL CLI CHECKS PASSED%s\n' "$C_GREEN" "$C_RST"
exit 0
