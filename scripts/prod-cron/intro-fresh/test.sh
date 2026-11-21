#!/bin/sh
# Offline harness for run.sh beside it: fake docker / flock / shred / alert on
# PATH, so it never reaches Docker, the network or a database.
#
#   sh scripts/prod-cron/intro-fresh/test.sh
set -eu

RUN_SH=${RUN_SH:-"$(cd "$(dirname "$0")" && pwd)/run.sh"}
FAILED=0

tstart() { T_ID=$1; T_OK=1; T_MSG=""; }
fail() { T_OK=0; T_MSG="$T_MSG $*"; }
tend() {
  if [ "$T_OK" = 1 ]; then echo "T$T_ID PASS"; else echo "T$T_ID FAIL:$T_MSG"; FAILED=1; fi
}

setup() {
  td=$(mktemp -d)
  mkdir -p "$td/bin" "$td/base" "$td/mt" "$td/ts" "$td/ctl/rc"
  : > "$td/mt/app.env"
  : > "$td/mt/llm.env"
  : > "$td/llm.env"
  cat > "$td/bin/docker" <<'FAKE'
#!/bin/sh
set -eu
CTL=${FAKE_DOCKER_DIR:?refusing to reach a real docker}
printf '%s\n' "$*" >> "$CTL/docker.args"
case "$1" in
  pull) exit 0 ;;
  image) echo "ghcr.io/next-moe/infra-tools@sha256:cafe"; exit 0 ;;
  inspect) printf 'KUN_PG_HOST=postgres\nKUN_PG_PORT=5432\n'; exit 0 ;;
  run)
    lane=$(printf '%s\n' "$@" | sed -n 's/^intro-fresh-//p' | head -n 1)
    echo "$lane" >> "$CTL/lanes"
    if [ -f "$CTL/rc/$lane" ]; then exit "$(cat "$CTL/rc/$lane")"; fi
    exit 0
    ;;
esac
echo "fake docker: unexpected $*" >&2
exit 99
FAKE
  # Call 1 is the job's own lock; calls 2 and 3 probe the nightlies' locks.
  cat > "$td/bin/flock" <<'FAKE'
#!/bin/sh
CTL=${FAKE_DOCKER_DIR:?}
n=$(( $(cat "$CTL/flock.n" 2>/dev/null || echo 0) + 1 ))
echo "$n" > "$CTL/flock.n"
[ -f "$CTL/busy.$n" ] && exit 1
exit 0
FAKE
  printf '#!/bin/sh\n[ "$1" = -u ] && shift\nrm -f "$@"\n' > "$td/bin/shred"
  printf '#!/bin/sh\necho "$1" >> "%s/ctl/alerts"\n' "$td" > "$td/alert"
  chmod +x "$td/bin/"* "$td/alert"
}

run_job() {
  rc=0
  PATH="$td/bin:$PATH" FAKE_DOCKER_DIR="$td/ctl" FRESH_BASE="$td/base" FRESH_ALERT="$td/alert" \
    FRESH_MT_BASE="$td/mt" FRESH_TS_BASE="$td/ts" FRESH_TS_LLM_ENV="$td/llm.env" sh "$RUN_SH" || rc=$?
}

tstart 1
setup
run_job
[ "$rc" = 0 ] || fail "exit $rc"
want="entity work-claimed-ja work-claimed-en work-bodyless-ja work-bodyless-en safety-intros safety-char-intros "
[ "$(tr '\n' ' ' < "$td/ctl/lanes")" = "$want" ] || fail "lanes: $(tr '\n' ' ' < "$td/ctl/lanes")"
[ "$(grep -c -- '--changed-since 2h' "$td/ctl/docker.args")" = 5 ] || fail "an MT lane runs without the window"
! grep -q -- '-mode titles' "$td/ctl/docker.args" || fail "the title lanes belong to the nightly"
[ -f "$td/base/state/last-success" ] || fail "no stamp"
[ ! -f "$td/ctl/alerts" ] || fail "alerted on success"
[ ! -f "$td/base/env.tmp" ] || fail "env.tmp left behind"
tend

tstart 2
setup
touch "$td/ctl/busy.2"
run_job
[ "$rc" = 0 ] || fail "a busy nightly is not a failure (exit $rc)"
[ ! -f "$td/ctl/lanes" ] || fail "ran lanes beside intromt-nightly"
tend

tstart 3
setup
touch "$td/ctl/busy.3"
run_job
[ ! -f "$td/ctl/lanes" ] || fail "ran lanes beside text-safety-nightly"
tend

tstart 4
setup
echo 1 > "$td/ctl/rc/work-claimed-ja"
run_job
[ "$rc" != 0 ] || fail "a failed lane exited 0"
grep -qx safety-char-intros "$td/ctl/lanes" || fail "later lanes skipped after a failure"
[ ! -f "$td/base/state/last-success" ] || fail "stamped a failed run"
grep -q FAIL "$td/ctl/alerts" 2>/dev/null || fail "no alert"
tend

exit "$FAILED"
