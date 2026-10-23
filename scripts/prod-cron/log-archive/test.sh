#!/bin/sh
# Offline harness for run.sh beside it: fake docker / flock / alert on PATH,
# so it never reaches Docker.
#
#   sh scripts/prod-cron/log-archive/test.sh
set -eu

RUN_SH="$(cd "$(dirname "$0")" && pwd)/run.sh"
FAILED=0

tstart() { T_ID=$1; T_OK=1; T_MSG=""; }
fail() { T_OK=0; T_MSG="$T_MSG $*"; }
tend() {
  if [ "$T_OK" = 1 ]; then echo "T$T_ID PASS"; else echo "T$T_ID FAIL:$T_MSG"; FAILED=1; fi
}

# ctl/names lists running containers; ctl/started.<name> its StartedAt;
# ctl/logs.<name> what `docker logs` prints; ctl/fail.<name> makes it fail.
install_fakes() {
  td=$1
  mkdir -p "$td/bin" "$td/base" "$td/ctl"
  cat > "$td/bin/docker" <<'FAKE'
#!/bin/sh
set -eu
CTL=${FAKE_CTL:?FAKE_CTL unset — refusing to reach a real docker}
echo "$*" >> "$CTL/docker.log"
case "$1" in
  ps) cat "$CTL/names" ;;
  inspect) eval "n=\${$#}"; cat "$CTL/started.$n" ;;
  logs)
    eval "n=\${$#}"
    [ -f "$CTL/fail.$n" ] && { echo "Error: No such container: $n" >&2; exit 1; }
    cat "$CTL/logs.$n" 2>/dev/null || true ;;
  *) echo "fake docker: unexpected $*" >&2; exit 99 ;;
esac
FAKE
  cat > "$td/bin/flock" <<'FAKE'
#!/bin/sh
[ "${FLOCK_HOLD_FAIL:-0}" = 1 ] && exit 1
exit 0
FAKE
  cat > "$td/alert.sh" <<'FAKE'
#!/bin/sh
echo "$1" >> "${FAKE_CTL:?}/alerts"
FAKE
  chmod +x "$td/bin/"* "$td/alert.sh"
}

run_job() {
  td=$1
  set +e
  env PATH="$td/bin:$PATH" FAKE_CTL="$td/ctl" \
    LOG_ARCHIVE_BASE="$td/base" LOG_ARCHIVE_ALERT="$td/alert.sh" \
    sh "$RUN_SH" > "$td/ctl/stdout" 2>&1
  echo $? > "$td/ctl/exit"
  set -e
}

exit_is() { [ "$(cat "$1/ctl/exit")" = "$2" ]; }
archived() { cat "$1"/base/archive/"$2"/*.log.gz 2>/dev/null | gzip -dc; }
logs_call() { grep "^logs .* $2\$" "$1/ctl/docker.log" | tail -1; }

# --- T1: first run archives each container from its start and stamps ---
tstart 1
td=$(mktemp -d); install_fakes "$td"
printf 'oauth-1\nredis-1\n' > "$td/ctl/names"
echo 2026-10-23T07:54:28.538859588Z > "$td/ctl/started.oauth-1"
echo 2026-10-17T18:35:01.1Z > "$td/ctl/started.redis-1"
echo "line a" > "$td/ctl/logs.oauth-1"
run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
[ -f "$td/base/state/last-success" ] || fail "no stamp"
[ "$(archived "$td" oauth-1)" = "line a" ] || fail "oauth not archived"
[ ! -d "$td/base/archive/redis-1" ] || fail "empty log produced an archive"
logs_call "$td" oauth-1 | grep -q -- '--since 2026-10-23T07:54:28Z' || fail "first since not the start: $(logs_call "$td" oauth-1)"
[ -s "$td/base/since/redis-1" ] || fail "empty log left no mark"
tend

# --- T2: the next run starts at the mark and appends ---
tstart 2
mark=$(cat "$td/base/since/oauth-1")
echo "line b" > "$td/ctl/logs.oauth-1"
run_job "$td"
logs_call "$td" oauth-1 | grep -q -- "--since $mark" || fail "since not the mark: $(logs_call "$td" oauth-1)"
[ "$(archived "$td" oauth-1 | tr '\n' ' ')" = "line a line b " ] || fail "not appended: $(archived "$td" oauth-1)"
tend

# --- T3: a container recreated after the mark starts at its new start ---
tstart 3
echo 2099-01-01T00:00:00.5Z > "$td/ctl/started.oauth-1"
run_job "$td"
logs_call "$td" oauth-1 | grep -q -- '--since 2099-01-01T00:00:00Z' || fail "recreate not honoured: $(logs_call "$td" oauth-1)"
tend

# --- T4: one container failing does not stop the others, keeps its mark, and does not fail the run ---
tstart 4
echo 2026-10-23T07:54:28Z > "$td/ctl/started.oauth-1"
before=$(cat "$td/base/since/redis-1")
cp "$td/base/since/oauth-1" "$td/ctl/oauth-mark-before"
touch "$td/ctl/fail.oauth-1"
echo "redis line" > "$td/ctl/logs.redis-1"
run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
cmp -s "$td/base/since/oauth-1" "$td/ctl/oauth-mark-before" || fail "failed container's mark moved"
[ "$(archived "$td" redis-1)" = "redis line" ] || fail "redis skipped"
[ "$(cat "$td/base/since/redis-1")" != "$before" ] || [ "$before" = "$(cat "$td/base/since/redis-1")" ] || fail "redis mark"
grep -q 'archived=1 skipped=1' "$td"/base/logs/run-*.log || fail "summary"
tend; rm -rf "$td"

# --- T5: docker ps failing alerts and leaves no stamp ---
tstart 5
td=$(mktemp -d); install_fakes "$td"
run_job "$td"
exit_is "$td" 0 && fail "exit 0 without docker ps"
grep -q '^\[FAIL\] log-archive' "$td/ctl/alerts" 2>/dev/null || fail "no alert"
[ ! -f "$td/base/state/last-success" ] || fail "stamped"
tend; rm -rf "$td"

# --- T6: a held lock exits quietly without touching docker ---
tstart 6
td=$(mktemp -d); install_fakes "$td"; printf 'oauth-1\n' > "$td/ctl/names"
FLOCK_HOLD_FAIL=1 run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
[ ! -f "$td/ctl/docker.log" ] || fail "touched docker"
tend; rm -rf "$td"

exit "$FAILED"
