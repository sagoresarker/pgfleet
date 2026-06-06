#!/usr/bin/env bash
# scripts/e2e-test.sh — PgFleet production-readiness test suite
#
# Runs all 7 tiers strictly one at a time, so each gets the full machine
# (concurrent tiers OOM-kill restore/cluster containers on a constrained host).
# Exit 0 = all passed. Exit 1 = one or more failed.
#
# Configuration via env vars:
#   PGFLEET_API_URL       default: http://localhost:8080
#   PGFLEET_ADMIN_EMAIL   default: admin@pgfleet.local
#   PGFLEET_ADMIN_PASSWORD default: change-me-please
set -uo pipefail

# ─── Config ───────────────────────────────────────────────────────────────────
API_URL="${PGFLEET_API_URL:-http://localhost:8080}"
API_EMAIL="${PGFLEET_ADMIN_EMAIL:-admin@pgfleet.local}"
API_PASSWORD="${PGFLEET_ADMIN_PASSWORD:-change-me-please}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
LOG_DIR="$ROOT_DIR/logs/e2e"
BIN_DIR="$ROOT_DIR/bin"
LOADGEN="$BIN_DIR/loadgen"

TOKEN=""
HAS_GCC=true
START_TIME=$(date +%s)
# Unique 6-char hex suffix per run so each run gets fresh S3 stanza paths.
# Deleting an instance removes the DB record and container but leaves the
# pgBackRest stanza directory in the bucket. Reusing the same stanza name
# with a brand-new Postgres (different system ID) causes stanza-create [028].
RUN_ID=$(openssl rand -hex 3)

declare -A TIER_NAME=(
  [1]="Unit tests"
  [2]="Integration suite"
  [3]="Consistency oracle"
  [4]="Backup + restore"
  [5]="PITR fidelity"
  [6]="HA failover"
  [7]="Control-plane resilience"
)

# ─── Colours ──────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

log()  { echo -e "${CYAN}[$(date '+%H:%M:%S')]${RESET} $*"; }
warn() { echo -e "${YELLOW}[$(date '+%H:%M:%S')] WARN${RESET} $*"; }
err()  { echo -e "${RED}[$(date '+%H:%M:%S')] ERROR${RESET} $*" >&2; }
tlog() { echo -e "${CYAN}[T$1 $(date '+%H:%M:%S')]${RESET} ${*:2}"; }  # tier log

# ─── Prerequisite check ───────────────────────────────────────────────────────
detect_pm() {
  command -v apt-get &>/dev/null && { echo apt; return; }
  command -v dnf     &>/dev/null && { echo dnf; return; }
  command -v brew    &>/dev/null && { echo brew; return; }
  echo unknown
}

# install_if_missing BINARY PKG_APT PKG_DNF PKG_BREW
install_if_missing() {
  local bin=$1 apt=$2 dnf=$3 brew=$4
  command -v "$bin" &>/dev/null && return 0
  local pm; pm=$(detect_pm)
  log "Installing $bin ($pm)..."
  case $pm in
    apt)  sudo apt-get install -y "$apt"  >/dev/null 2>&1 ;;
    dnf)  sudo dnf install -y "$dnf"      >/dev/null 2>&1 ;;
    brew) brew install "$brew"            >/dev/null 2>&1 ;;
    *)    return 1 ;;
  esac
}

prereq_check() {
  log "Checking prerequisites..."
  local errors=0

  # Hard dependencies — abort if missing
  if ! command -v docker &>/dev/null; then
    err "docker is required but not found."
    err "  → https://docs.docker.com/engine/install/"
    (( errors++ ))
  elif ! docker info &>/dev/null; then
    err "Docker is installed but not running. Start Docker and retry."
    (( errors++ ))
  fi

  if ! command -v go &>/dev/null; then
    err "go is required but not found."
    err "  → https://go.dev/dl/"
    (( errors++ ))
  fi

  # Auto-installable tools
  install_if_missing curl    curl              curl         curl       \
    || { err "Failed to install curl";    (( errors++ )); }
  install_if_missing jq      jq                jq           jq         \
    || { err "Failed to install jq";     (( errors++ )); }
  install_if_missing make    build-essential   make         make       \
    || { err "Failed to install make";   (( errors++ )); }
  install_if_missing psql    postgresql-client postgresql   libpq      \
    || { err "Failed to install psql";   (( errors++ )); }
  install_if_missing openssl openssl           openssl      openssl    \
    || { err "Failed to install openssl"; (( errors++ )); }

  # gcc — soft dep for race detector; fall back gracefully
  if ! command -v gcc &>/dev/null; then
    warn "gcc not found — attempting install..."
    if install_if_missing gcc gcc gcc gcc 2>/dev/null && command -v gcc &>/dev/null; then
      log "gcc installed."
    else
      warn "gcc not available — unit tests will run WITHOUT -race detector."
      HAS_GCC=false
    fi
  fi

  # API reachability
  if ! curl -sf --max-time 5 "$API_URL/healthz" &>/dev/null; then
    err "PgFleet API at $API_URL is not reachable."
    err "  Run: make run   (then retry this script)"
    (( errors++ ))
  fi

  if (( errors > 0 )); then
    err "$errors prerequisite(s) missing — fix the above and retry."
    exit 1
  fi
  log "Prerequisites OK."
}

# ─── Build ────────────────────────────────────────────────────────────────────
build_loadgen() {
  log "Building loadgen..."
  mkdir -p "$BIN_DIR"
  ( cd "$ROOT_DIR" && go build -o "$LOADGEN" ./cmd/loadgen ) \
    || { err "loadgen build failed"; exit 1; }
  log "loadgen → $LOADGEN"
}

# ─── API helpers ──────────────────────────────────────────────────────────────
api_login() {
  local resp
  resp=$(curl -sf --max-time 10 -X POST "$API_URL/api/v1/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"email\":\"$API_EMAIL\",\"password\":\"$API_PASSWORD\"}") \
    || { err "Login request failed"; exit 1; }
  TOKEN=$(echo "$resp" | jq -r '.token // empty')
  [[ -n "$TOKEN" ]] || { err "Login failed — check PGFLEET_ADMIN_EMAIL / _PASSWORD"; exit 1; }
  export TOKEN
  log "Authenticated as $API_EMAIL"
}

# api METHOD /path [body]  — returns response body; exits non-zero on HTTP error
api() {
  local method=$1 path=$2 body=${3:-}
  local args=(-sf --max-time 30 -X "$method"
              "$API_URL$path"
              -H "Authorization: Bearer $TOKEN"
              -H "Content-Type: application/json")
  [[ -n "$body" ]] && args+=(-d "$body")
  curl "${args[@]}"
}

# provision NAME REPO_TYPE → prints instance ID; registers for cleanup
provision() {
  local name=$1 repo_type=$2
  local resp
  resp=$(api POST /api/v1/instances \
    "{\"name\":\"$name\",\"repo_type\":\"$repo_type\",\"pg_version\":\"16\",\"password\":\"E2eTestPass1!\"}") \
    || return 1
  local id; id=$(echo "$resp" | jq -r '.instance.id // empty')
  [[ -n "$id" ]] || { err "No instance ID in: $resp"; return 1; }
  echo "$id" >> "$LOG_DIR/cleanup_instances.txt"
  echo "$id"
}

# provision_cluster NAME → prints cluster ID; registers for cleanup
provision_cluster() {
  local name=$1
  local resp
  resp=$(api POST /api/v1/clusters \
    "{\"name\":\"$name\",\"replicas\":1,\"repo_type\":\"s3\",\"pg_version\":\"16\",\"password\":\"E2eTestPass1!\",\"pool_mode\":\"transaction\"}") \
    || return 1
  local id; id=$(echo "$resp" | jq -r '.cluster.id // empty')
  [[ -n "$id" ]] || { err "No cluster ID in: $resp"; return 1; }
  echo "$id" >> "$LOG_DIR/cleanup_clusters.txt"
  echo "$id"
}

# wait_status INSTANCE_ID WANTED_STATUS [TIMEOUT_SECS=600]
wait_status() {
  local id=$1 want=$2 timeout=${3:-600} elapsed=0 got
  while (( elapsed < timeout )); do
    got=$(api GET "/api/v1/instances/$id" 2>/dev/null | jq -r '.instance.status // empty')
    [[ "$got" == "$want" ]]  && return 0
    if [[ "$got" == "error" ]]; then
      local reason
      reason=$(api GET "/api/v1/instances/$id" 2>/dev/null | jq -r '.instance.last_error // "unknown"')
      err "Instance $id entered error state: $reason"
      return 1
    fi
    sleep 5; (( elapsed += 5 ))
  done
  err "Instance $id did not reach '$want' in ${timeout}s (last: $got)"
  return 1
}

# wait_cluster_status CLUSTER_ID WANTED [TIMEOUT=900]
wait_cluster_status() {
  local id=$1 want=$2 timeout=${3:-900} elapsed=0 got
  while (( elapsed < timeout )); do
    got=$(api GET "/api/v1/clusters/$id" 2>/dev/null | jq -r '.cluster.status // empty')
    [[ "$got" == "$want" ]] && return 0
    if [[ "$got" == "error" ]]; then
      local reason
      reason=$(api GET "/api/v1/clusters/$id" 2>/dev/null | jq -r '.cluster.last_error // "unknown"')
      err "Cluster $id entered error state: $reason"
      return 1
    fi
    sleep 5; (( elapsed += 5 ))
  done
  err "Cluster $id did not reach '$want' in ${timeout}s"
  return 1
}

# trigger_backup INSTANCE_ID — fires a full backup and waits for catalog to grow
trigger_backup() {
  local id=$1
  local before
  before=$(api GET "/api/v1/instances/$id/backups" | jq '.backups | length')
  api POST "/api/v1/instances/$id/backups" '{"type":"full","annotation":"e2e-test"}' >/dev/null \
    || return 1
  local elapsed=0 timeout=600 after
  while (( elapsed < timeout )); do
    sleep 10; (( elapsed += 10 ))
    after=$(api GET "/api/v1/instances/$id/backups" 2>/dev/null | jq '.backups | length')
    (( after > before )) && return 0
  done
  err "Backup for $id did not appear in catalog within ${timeout}s"
  return 1
}

get_dsn()         { api GET "/api/v1/instances/$1/connection" | jq -r '.dsn // empty'; }
get_cluster_dsn() { api GET "/api/v1/clusters/$1/connection"  | jq -r '.dsn // empty'; }

# wait_postgres_ready DSN [TIMEOUT_SECS=120]
# Polls psql until Postgres accepts connections. Called after wait_status to
# bridge the gap between pgfleet marking an instance "running" and the Postgres
# process inside the container being ready to accept new connections.
wait_postgres_ready() {
  local dsn=$1 timeout=${2:-120} elapsed=0
  while (( elapsed < timeout )); do
    psql "$dsn" -q -t -c "SELECT 1" &>/dev/null && return 0
    sleep 2; (( elapsed += 2 ))
  done
  err "Postgres at $dsn did not accept connections within ${timeout}s"
  return 1
}

loadgen_run() {
  local dsn=$1 mode=$2; shift 2
  "$LOADGEN" -dsn "$dsn" -mode "$mode" "$@"
}

# free_instance ID / free_cluster ID — delete a tier's own resources as soon as
# it finishes (best-effort), so the containers are released for later tiers
# instead of all lingering until the final EXIT-trap cleanup. Without this, the
# last tier in the chain restores on a box still loaded with every prior tier's
# instances/clusters, and Postgres startup can exceed the 90s readyTimeout.
free_instance() {
  [[ -z "${1:-}" ]] && return 0
  api DELETE "/api/v1/instances/$1" &>/dev/null || true
}
free_cluster() {
  [[ -z "${1:-}" ]] && return 0
  api DELETE "/api/v1/clusters/$1" &>/dev/null || true
}

# wait_router_ready DSN [TIMEOUT=120] — poll a cluster router DSN until a query
# succeeds. After a failover the controller repoints PgCat to the new primary;
# the new router needs a few seconds before it stops returning AllServersDown.
wait_router_ready() {
  local dsn=$1 timeout=${2:-120} elapsed=0
  while (( elapsed < timeout )); do
    psql "$dsn" -q -t -c "SELECT 1" &>/dev/null && return 0
    sleep 3; (( elapsed += 3 ))
  done
  err "Router at $dsn did not become ready within ${timeout}s"
  return 1
}

# ─── Pre-run teardown ─────────────────────────────────────────────────────────
# Removes stale e2e-* instances/clusters left by a previous aborted run so
# name conflicts don't cause instant provisioning failures.
teardown_stale() {
  log "Checking for stale e2e-* resources from previous runs..."
  local found=0

  local cluster_ids
  cluster_ids=$(api GET /api/v1/clusters 2>/dev/null \
    | jq -r '.clusters[]? | select(.name | startswith("e2e-")) | .id')
  for id in $cluster_ids; do
    api DELETE "/api/v1/clusters/$id" 2>/dev/null && log "  Removed stale cluster $id" || true
    (( found++ ))
  done

  # Catch any orphaned member instances after cluster delete
  local inst_ids
  inst_ids=$(api GET /api/v1/instances 2>/dev/null \
    | jq -r '.instances[]? | select(.name | startswith("e2e-")) | .id')
  for id in $inst_ids; do
    api DELETE "/api/v1/instances/$id" 2>/dev/null && log "  Removed stale instance $id" || true
    (( found++ ))
  done

  (( found > 0 )) && log "Stale resource teardown complete ($found removed)." \
                  || log "No stale e2e-* resources found."
}

# ─── Tier result helpers (file-based — called from subshells) ─────────────────
mark_pass() { echo 0 > "$LOG_DIR/tier$1.rc"; }
mark_fail() { echo 1 > "$LOG_DIR/tier$1.rc"; }
mark_time() { echo $(( $(date +%s) - $2 )) > "$LOG_DIR/tier$1.time"; }

# ─── Cleanup ──────────────────────────────────────────────────────────────────
cleanup() {
  log "Cleaning up e2e test resources..."
  # Re-login in case token expired during a long run
  { api_login 2>/dev/null; } || true

  local f="$LOG_DIR/cleanup_instances.txt"
  if [[ -f "$f" ]]; then
    sort -u "$f" | while IFS= read -r id; do
      [[ -z "$id" ]] && continue
      api DELETE "/api/v1/instances/$id" 2>/dev/null && log "  Deleted instance $id" || \
        warn "  Could not delete instance $id (may already be gone)"
    done
  fi

  local g="$LOG_DIR/cleanup_clusters.txt"
  if [[ -f "$g" ]]; then
    sort -u "$g" | while IFS= read -r id; do
      [[ -z "$id" ]] && continue
      api DELETE "/api/v1/clusters/$id" 2>/dev/null && log "  Deleted cluster $id" || \
        warn "  Could not delete cluster $id"
    done
  fi
}

# ─── Tier 1: Unit tests ───────────────────────────────────────────────────────
run_tier1() {
  local t=1; local t0=$(date +%s)
  tlog $t "Starting: ${TIER_NAME[$t]}"
  cd "$ROOT_DIR"

  local rc=0
  if $HAS_GCC; then
    tlog $t "Running: go test -race ./..."
    go test -race ./... || rc=$?
  else
    tlog $t "Running: go test ./... (no -race; gcc unavailable)"
    go test ./... || rc=$?
  fi

  if (( rc == 0 )); then tlog $t "PASS"; mark_pass $t
  else                    tlog $t "FAIL"; mark_fail $t; fi
  mark_time $t $t0
}

# ─── Tier 2: Integration suite ────────────────────────────────────────────────
run_tier2() {
  local t=2; local t0=$(date +%s)
  tlog $t "Starting: ${TIER_NAME[$t]}"
  cd "$ROOT_DIR"

  if make test-integration; then tlog $t "PASS"; mark_pass $t
  else                           tlog $t "FAIL"; mark_fail $t; fi
  mark_time $t $t0
}

# ─── Tier 3: Consistency oracle ───────────────────────────────────────────────
run_tier3() {
  local t=3; local t0=$(date +%s)
  tlog $t "Starting: ${TIER_NAME[$t]}"
  cd "$ROOT_DIR"

  local id; id=$(provision "e2e-c-$RUN_ID" "s3") || { mark_fail $t; mark_time $t $t0; return; }
  tlog $t "Waiting for instance $id to reach running..."
  wait_status "$id" "running" 600 || { mark_fail $t; mark_time $t $t0; return; }

  local dsn; dsn=$(get_dsn "$id")
  tlog $t "Running: seed → churn (3 min) → verify"
  if loadgen_run "$dsn" all \
      -accounts 20000 -events 300000 -workers 12 -duration 3m; then
    tlog $t "PASS — consistency invariant holds"
    mark_pass $t
  else
    tlog $t "FAIL — consistency invariant violated (torn transaction)"
    mark_fail $t
  fi
  free_instance "$id"   # release containers immediately for later tiers
  mark_time $t $t0
}

# ─── Tier 4: Backup + restore ─────────────────────────────────────────────────
run_tier4() {
  local t=4; local t0=$(date +%s)
  tlog $t "Starting: ${TIER_NAME[$t]}"
  cd "$ROOT_DIR"

  local id; id=$(provision "e2e-r-$RUN_ID" "s3") || { mark_fail $t; mark_time $t $t0; return; }
  tlog $t "Waiting for instance $id..."
  wait_status "$id" "running" 600 || { mark_fail $t; mark_time $t $t0; return; }

  local dsn; dsn=$(get_dsn "$id")

  tlog $t "Seeding data (batch 1)..."
  loadgen_run "$dsn" seed -accounts 10000 -events 200000 \
    || { mark_fail $t; mark_time $t $t0; return; }

  tlog $t "Taking full backup..."
  trigger_backup "$id" || { mark_fail $t; mark_time $t $t0; return; }

  tlog $t "Running post-backup churn (90 s)..."
  # -accounts MUST match the seed (10000): loadgen generates event account_ids
  # in [1,accounts]; defaulting to 100000 would create out-of-range ids and
  # orphan events that the full verify (correctly) rejects.
  loadgen_run "$dsn" churn -accounts 10000 -workers 6 -duration 90s \
    || { mark_fail $t; mark_time $t $t0; return; }

  tlog $t "Restoring from latest backup..."
  api POST "/api/v1/instances/$id/restore" \
    '{"type":"","target":"","delta":false}' >/dev/null \
    || { mark_fail $t; mark_time $t $t0; return; }
  wait_status "$id" "running" 900 || { mark_fail $t; mark_time $t $t0; return; }
  # Re-fetch DSN: restore creates a brand-new container (new name, new port).
  dsn=$(get_dsn "$id")
  wait_postgres_ready "$dsn" 120  || { mark_fail $t; mark_time $t $t0; return; }

  tlog $t "Verifying consistency after restore (full invariant: pot + balances + orphans)..."
  if loadgen_run "$dsn" verify -accounts 10000; then
    tlog $t "PASS — pot conserved, no negative balances, no orphan events"
    mark_pass $t
  else
    tlog $t "FAIL — consistency invariant violated after restore"
    mark_fail $t
  fi
  free_instance "$id"   # release before the next tier starts
  mark_time $t $t0
}

# ─── Tier 5: PITR fidelity ────────────────────────────────────────────────────
run_tier5() {
  local t=5; local t0=$(date +%s)
  tlog $t "Starting: ${TIER_NAME[$t]}"
  cd "$ROOT_DIR"

  local id; id=$(provision "e2e-p-$RUN_ID" "s3") || { mark_fail $t; mark_time $t $t0; return; }
  tlog $t "Waiting for instance $id..."
  wait_status "$id" "running" 600 || { mark_fail $t; mark_time $t $t0; return; }

  local dsn; dsn=$(get_dsn "$id")

  tlog $t "Seeding batch 1..."
  loadgen_run "$dsn" seed -accounts 5000 -events 100000 \
    || { mark_fail $t; mark_time $t $t0; return; }

  tlog $t "Taking full backup (WAL must archive before PITR target)..."
  trigger_backup "$id" || { mark_fail $t; mark_time $t $t0; return; }

  # Capture the PITR target from the SERVER'S OWN CLOCK (timezone-aware, e.g.
  # 2026-06-06 04:19:19.42+00). Generating it on the test host with `date` is
  # unreliable: the managed Postgres runs in UTC, so a host in UTC+N would yield
  # a target N hours off, and recovery would over/undershoot. Using now() from
  # the instance removes all host-vs-server timezone ambiguity.
  sleep 5
  local pitr_time; pitr_time=$(psql "$dsn" -tAq -c "SELECT now()")
  tlog $t "PITR target (server clock): $pitr_time"
  sleep 5  # gap before the canary so the target is unambiguously before it

  # Insert a canary row that must NOT survive the PITR restore
  tlog $t "Inserting post-target canary row..."
  psql "$dsn" -q -c \
    "INSERT INTO loadgen_events(account_id,kind,amount,payload,created_at)
     VALUES (1,'pitr_canary',0,'{}',now())" \
    || { mark_fail $t; mark_time $t $t0; return; }

  tlog $t "Running batch 2 churn (60 s — must NOT survive restore)..."
  # -accounts MUST match the seed (5000) so churn does not create out-of-range
  # account_ids / orphan events that the full verify would reject.
  loadgen_run "$dsn" churn -accounts 5000 -workers 4 -duration 60s \
    || { mark_fail $t; mark_time $t $t0; return; }

  # CRITICAL for PITR-from-S3: recovery replays WAL until it finds a commit
  # at/after the target, then promotes. That stop-point WAL segment must be
  # ARCHIVED to the object store or archive-get can't fetch it and recovery
  # stalls. Force a WAL switch and wait so the post-target WAL is in the repo.
  tlog $t "Forcing WAL switch + archive so post-target WAL is in the repo..."
  psql "$dsn" -q -c "SELECT pg_switch_wal()" >/dev/null 2>&1 || true
  psql "$dsn" -q -c "CHECKPOINT" >/dev/null 2>&1 || true
  sleep 10  # let archive_command push the switched segment to S3

  tlog $t "Restoring to PITR target: $pitr_time"
  api POST "/api/v1/instances/$id/restore" \
    "{\"type\":\"time\",\"target\":\"$pitr_time\",\"delta\":false}" >/dev/null \
    || { mark_fail $t; mark_time $t $t0; return; }
  wait_status "$id" "running" 900 || { mark_fail $t; mark_time $t $t0; return; }
  # Re-fetch DSN: restore creates a brand-new container (new name, new port).
  dsn=$(get_dsn "$id")
  wait_postgres_ready "$dsn" 180  || { mark_fail $t; mark_time $t $t0; return; }

  # Check 1: canary row must be gone (proves PITR landed before the canary insert)
  tlog $t "Checking canary row is absent..."
  local canary_count
  canary_count=$(psql "$dsn" -t -q \
    -c "SELECT COUNT(*) FROM loadgen_events WHERE kind='pitr_canary'" \
    | tr -d '[:space:]')

  # Check 2: full consistency invariant must hold (pot + balances + orphans).
  tlog $t "Checking full consistency invariant after PITR..."
  local verify_ok=true
  loadgen_run "$dsn" verify -accounts 5000 || verify_ok=false

  if [[ "$canary_count" == "0" ]] && $verify_ok; then
    tlog $t "PASS — PITR landed at correct point; canary absent; invariant holds"
    mark_pass $t
  elif [[ "$canary_count" != "0" ]]; then
    tlog $t "FAIL — canary row survived PITR (restore landed too late)"
    mark_fail $t
  else
    tlog $t "FAIL — consistency invariant broken after PITR"
    mark_fail $t
  fi
  free_instance "$id"
  mark_time $t $t0
}

# ─── Tier 6: HA failover ──────────────────────────────────────────────────────
run_tier6() {
  local t=6; local t0=$(date +%s)
  tlog $t "Starting: ${TIER_NAME[$t]}"
  cd "$ROOT_DIR"

  local cluster_id
  cluster_id=$(provision_cluster "e2e-fa-$RUN_ID") \
    || { mark_fail $t; mark_time $t $t0; return; }
  tlog $t "Cluster $cluster_id created — waiting for running..."
  wait_cluster_status "$cluster_id" "running" 900 \
    || { mark_fail $t; mark_time $t $t0; return; }
  # Allow replication to catch up and PgCat to fully stabilise before sending
  # writes — a COPY immediately after cluster-ready can hit a replica that hasn't
  # replicated the schema yet, causing "relation does not exist".
  tlog $t "Waiting 15 s for replication and router to stabilise..."
  sleep 15

  local cluster_dsn
  cluster_dsn=$(get_cluster_dsn "$cluster_id")

  # Seed directly on the primary to avoid PgCat routing the COPY statement
  # description to a replica in transaction pool mode (extended query protocol
  # quirk where DESCRIBE can land on a different backend than EXECUTE).
  tlog $t "Seeding data on primary directly..."
  local primary_id primary_dsn
  primary_id=$(api GET "/api/v1/clusters/$cluster_id" \
    | jq -r '.members[] | select(.role=="primary") | .id')
  primary_dsn=$(get_dsn "$primary_id")
  loadgen_run "$primary_dsn" seed -accounts 10000 -events 200000 \
    || { mark_fail $t; mark_time $t $t0; return; }

  # Identify the primary instance
  local primary_name
  primary_name=$(api GET "/api/v1/clusters/$cluster_id" \
    | jq -r '.members[] | select(.role=="primary") | .name')
  tlog $t "Primary: $primary_name"

  # Start churn in background (-accounts matches the seed so the full verify is
  # valid). 3 min comfortably covers the kill-at-30s + up-to-6-min promotion poll.
  tlog $t "Starting background churn (3 min)..."
  loadgen_run "$cluster_dsn" churn -accounts 10000 -workers 8 -duration 3m &
  local churn_pid=$!

  # Let churn warm up, then kill the primary
  sleep 30
  local container="pgfleet-pg-$primary_name"
  tlog $t "Killing primary container: $container"
  if ! docker kill "$container" 2>/dev/null; then
    warn "docker kill failed — container may already be gone"
  fi

  # Wait for promotion (max 6 min — detection=3×30s=90s + promotion itself + buffer)
  tlog $t "Waiting for replica promotion (max 6 min)..."
  local elapsed=0 promoted=false new_primary=""
  while (( elapsed < 360 )); do
    sleep 10; (( elapsed += 10 ))
    local cluster_resp
    cluster_resp=$(api GET "/api/v1/clusters/$cluster_id" 2>/dev/null)
    new_primary=$(echo "$cluster_resp" \
      | jq -r --arg old "$primary_name" \
          '.members[] | select(.role=="primary" and .name != $old) | .name')
    if [[ -n "$new_primary" ]]; then
      tlog $t "Promoted: $new_primary"
      promoted=true; break
    fi
    if (( elapsed % 30 == 0 )); then
      local roles
      roles=$(echo "$cluster_resp" | jq -r '.members[] | "\(.name)=\(.role)"' | tr '\n' ' ')
      tlog $t "  [${elapsed}s] still waiting — current roles: $roles"
    fi
  done

  # Stop churn regardless
  kill "$churn_pid" 2>/dev/null; wait "$churn_pid" 2>/dev/null || true

  if ! $promoted; then
    tlog $t "FAIL — no replica promoted within 6 minutes"
    free_cluster "$cluster_id"; mark_fail $t; mark_time $t $t0; return
  fi

  # Data-safety check (AUTHORITATIVE): verify the money invariant against the NEW
  # PRIMARY DIRECTLY. The promoted data lives on the primary itself, independent
  # of the router — this is the real "no committed data lost" assertion.
  local new_primary_id new_primary_dsn
  new_primary_id=$(api GET "/api/v1/clusters/$cluster_id" \
    | jq -r '.members[] | select(.role=="primary") | .id')
  new_primary_dsn=$(get_dsn "$new_primary_id")
  if ! wait_postgres_ready "$new_primary_dsn" 120; then
    tlog $t "FAIL — new primary not accepting connections after promotion"
    free_cluster "$cluster_id"; mark_fail $t; mark_time $t $t0; return
  fi
  tlog $t "Verifying full invariant on the promoted primary (direct)..."
  if ! loadgen_run "$new_primary_dsn" verify -accounts 10000; then
    tlog $t "FAIL — data lost or corrupted on the promoted primary"
    free_cluster "$cluster_id"; mark_fail $t; mark_time $t $t0; return
  fi

  # Router repoint check: the controller repoints PgCat to the new primary and
  # marks the cluster RUNNING again. This is a hard assertion — if the repoint
  # fails (e.g. router-name conflict) the cluster stays "degraded", which means
  # the router was not actually reconfigured for the new topology even if PgCat's
  # stale config happens to still serve reads from a surviving backend.
  tlog $t "Waiting for cluster to return to running (router repointed)..."
  if ! wait_cluster_status "$cluster_id" "running" 180; then
    tlog $t "FAIL — cluster did not return to running after failover (repoint failed/degraded)"
    free_cluster "$cluster_id"; mark_fail $t; mark_time $t $t0; return
  fi
  local new_dsn; new_dsn=$(get_cluster_dsn "$cluster_id")
  if ! wait_router_ready "$new_dsn" 120; then
    tlog $t "FAIL — router did not serve queries after failover (repoint)"
    free_cluster "$cluster_id"; mark_fail $t; mark_time $t $t0; return
  fi

  # Confirm old primary is fenced (container must not be running)
  local old_running
  old_running=$(docker inspect "$container" 2>/dev/null \
    | jq -r '.[0].State.Running // "absent"')
  if [[ "$old_running" == "false" || "$old_running" == "absent" ]]; then
    tlog $t "PASS — failover clean, old primary fenced, no data loss"
    mark_pass $t
  else
    tlog $t "FAIL — old primary container is still running (split-brain risk)"
    mark_fail $t
  fi
  free_cluster "$cluster_id"
  mark_time $t $t0
}

# ─── Tier 7: Control-plane resilience ─────────────────────────────────────────
run_tier7() {
  local t=7; local t0=$(date +%s)
  tlog $t "Starting: ${TIER_NAME[$t]}"
  cd "$ROOT_DIR"

  # Provision a dedicated instance for this tier. Every other tier now frees its
  # own resources on completion, so we can't rely on their leftovers — and a
  # self-owned instance makes the resilience assertion deterministic.
  local own_id; own_id=$(provision "e2e-cp-$RUN_ID" "s3") \
    || { tlog $t "FAIL — could not provision probe instance"; mark_fail $t; mark_time $t $t0; return; }
  tlog $t "Waiting for probe instance $own_id..."
  wait_status "$own_id" "running" 600 \
    || { tlog $t "FAIL — probe instance never came up"; free_instance "$own_id"; mark_fail $t; mark_time $t $t0; return; }

  local running_ids=("$own_id") running_dsns=()
  local own_dsn; own_dsn=$(get_dsn "$own_id")
  wait_postgres_ready "$own_dsn" 120 \
    || { tlog $t "FAIL — probe instance not accepting connections"; free_instance "$own_id"; mark_fail $t; mark_time $t $t0; return; }
  running_dsns=("$own_dsn")
  tlog $t "Probe instance ready; testing survival across an API restart"

  # Find and kill the API process
  local api_pid
  api_pid=$(pgrep -f "pgfleet-api" | head -1 || true)
  if [[ -z "$api_pid" ]]; then
    tlog $t "pgfleet-api process not found — skipping kill/restart probe"
    mark_pass $t; mark_time $t $t0; return
  fi

  tlog $t "Sending SIGTERM to pgfleet-api (PID $api_pid)..."
  kill -TERM "$api_pid"
  sleep 3

  # Instances must still accept connections — they're independent Docker containers
  local probe_failures=0
  for dsn in "${running_dsns[@]}"; do
    if psql "$dsn" -q -t -c "SELECT 1" &>/dev/null; then
      tlog $t "  Instance still reachable after API kill"
    else
      tlog $t "  FAIL — instance unreachable: $dsn"
      (( probe_failures++ ))
    fi
  done

  # Restart the API
  tlog $t "Restarting pgfleet-api (make run)..."
  ( cd "$ROOT_DIR" && set -a; [[ -f .env ]] && . ./.env; set +a
    exec ./bin/pgfleet-api ) >> "$LOG_DIR/tier7.log" 2>&1 &

  # Wait for /healthz to come up
  local elapsed=0 alive=false
  while (( elapsed < 60 )); do
    sleep 2; (( elapsed += 2 ))
    curl -sf --max-time 3 "$API_URL/healthz" &>/dev/null && { alive=true; break; }
  done

  if ! $alive; then
    tlog $t "FAIL — API did not come back online within 60 s"
    mark_fail $t; mark_time $t $t0; return
  fi
  tlog $t "API is back online — re-authenticating..."
  api_login

  # All previously-reachable instances must reconcile back to running
  local reconcile_failures=0
  for id in "${running_ids[@]}"; do
    local status
    status=$(api GET "/api/v1/instances/$id" | jq -r '.instance.status')
    if [[ "$status" == "running" ]]; then
      tlog $t "  Instance $id reconciled: running"
    else
      tlog $t "  FAIL — instance $id shows '$status' after reconcile"
      (( reconcile_failures++ ))
    fi
  done

  if (( probe_failures + reconcile_failures == 0 )); then
    tlog $t "PASS — instances survived API restart; reconciler restored all state"
    mark_pass $t
  else
    tlog $t "FAIL — $probe_failures probe failures, $reconcile_failures reconcile failures"
    mark_fail $t
  fi
  mark_time $t $t0
}

# ─── Summary ──────────────────────────────────────────────────────────────────
fmt_duration() {
  local s=$1
  (( s < 60 )) && { echo "${s}s"; return; }
  printf "%dm%02ds" $(( s/60 )) $(( s%60 ))
}

print_summary() {
  local total_pass=0 total_fail=0
  local wall=$(( $(date +%s) - START_TIME ))

  echo ""
  echo -e "${BOLD}══════════════════════════════════════════════════${RESET}"
  echo -e "${BOLD}  PGFLEET PRODUCTION READINESS TEST RESULTS${RESET}"
  echo -e "${BOLD}══════════════════════════════════════════════════${RESET}"

  for t in 1 2 3 4 5 6 7; do
    local rc=1 dur=0
    [[ -f "$LOG_DIR/tier$t.rc" ]]   && rc=$(cat "$LOG_DIR/tier$t.rc")
    [[ -f "$LOG_DIR/tier$t.time" ]] && dur=$(cat "$LOG_DIR/tier$t.time")
    local d; d=$(fmt_duration "$dur")
    if [[ "$rc" == "0" ]]; then
      echo -e "  ${GREEN}✓${RESET}  Tier $t — ${TIER_NAME[$t]}  ${CYAN}[$d]${RESET}"
      (( total_pass++ ))
    else
      echo -e "  ${RED}✗${RESET}  Tier $t — ${TIER_NAME[$t]}  ${CYAN}[$d]${RESET}"
      echo -e "       ${YELLOW}↳ logs/e2e/tier${t}.log${RESET}"
      (( total_fail++ ))
    fi
  done

  echo -e "${BOLD}══════════════════════════════════════════════════${RESET}"
  local wall_fmt; wall_fmt=$(fmt_duration "$wall")
  echo -e "${BOLD}  $total_pass passed · $total_fail failed   (wall-clock: $wall_fmt)${RESET}"
  echo -e "${BOLD}══════════════════════════════════════════════════${RESET}"
  if (( total_fail == 0 )); then
    echo -e "  ${GREEN}${BOLD}✓ System is PRODUCTION READY${RESET}"
  else
    echo -e "  ${RED}${BOLD}✗ System is NOT production ready — fix failing tiers${RESET}"
  fi
  echo ""
  return "$total_fail"
}

# ─── Main ─────────────────────────────────────────────────────────────────────
main() {
  cd "$ROOT_DIR"
  mkdir -p "$LOG_DIR"
  rm -f "$LOG_DIR"/tier*.rc "$LOG_DIR"/tier*.time \
        "$LOG_DIR/cleanup_instances.txt" "$LOG_DIR/cleanup_clusters.txt"

  trap cleanup EXIT

  echo ""
  echo -e "${BOLD}══════════════════════════════════════════════════${RESET}"
  echo -e "${BOLD}  PGFLEET PRODUCTION READINESS TEST SUITE${RESET}"
  echo -e "${BOLD}══════════════════════════════════════════════════${RESET}"
  echo -e "  API: $API_URL    User: $API_EMAIL"
  echo -e "  Logs: $LOG_DIR"
  echo -e "${BOLD}══════════════════════════════════════════════════${RESET}"
  echo ""

  prereq_check
  build_loadgen
  api_login
  teardown_stale

  # Tiers run STRICTLY ONE AT A TIME. On a constrained host (7-8 GB) running
  # tiers concurrently competes for RAM — restore/swap/cluster containers pile
  # up and the kernel OOM-kills a container mid-operation (exit 137). Sequential
  # execution gives every tier the entire box, and each tier frees its own
  # instance/cluster before the next starts, so memory never accumulates.
  log "Running tiers sequentially (one at a time — each gets the full box)..."
  local spec tn fn rc dur
  for spec in 1:run_tier1 2:run_tier2 3:run_tier3 4:run_tier4 \
              5:run_tier5 6:run_tier6 7:run_tier7; do
    tn=${spec%%:*}; fn=${spec#*:}
    "$fn" > "$LOG_DIR/tier${tn}.log" 2>&1
    rc=1; [[ -f "$LOG_DIR/tier${tn}.rc" ]]   && rc=$(cat "$LOG_DIR/tier${tn}.rc")
    dur=0; [[ -f "$LOG_DIR/tier${tn}.time" ]] && dur=$(cat "$LOG_DIR/tier${tn}.time")
    if [[ "$rc" == "0" ]]; then
      log "Tier $tn (${TIER_NAME[$tn]}) — ${GREEN}PASS${RESET} [$(fmt_duration "$dur")]"
    else
      log "Tier $tn (${TIER_NAME[$tn]}) — ${RED}FAIL${RESET} [$(fmt_duration "$dur")]  → logs/e2e/tier${tn}.log"
    fi
  done

  print_summary
}

main "$@"
