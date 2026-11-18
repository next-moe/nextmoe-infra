#!/bin/sh
# Offline harness for run.sh beside it: fake docker / flock / shred / alert /
# reindex on PATH, so it never reaches Docker, the network or a database.
#
#   sh scripts/prod-cron/text-safety-nightly/test.sh
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
  mkdir -p "$td/bin" "$td/base" "$td/ctl/rc"
  printf 'KUN_LLM_GATEWAY_URL=http://ai:9284/v1\nKUN_LLM_GATEWAY_KEY=sentinel\n' > "$td/llm.env"
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
    lane=$(printf '%s\n' "$@" | sed -n 's/^text-safety-//p' | head -n 1)
    echo "$lane" >> "$CTL/lanes"
    if [ "$lane" = apply ] && [ -f "$CTL/apply.out" ]; then cat "$CTL/apply.out"; fi
    if [ -f "$CTL/rc/$lane" ]; then exit "$(cat "$CTL/rc/$lane")"; fi
    exit 0
    ;;
esac
echo "fake docker: unexpected $*" >&2
exit 99
FAKE
  printf '#!/bin/sh\nexit 0\n' > "$td/bin/flock"
  printf '#!/bin/sh\n[ "$1" = -u ] && shift\nrm -f "$@"\n' > "$td/bin/shred"
  printf '#!/bin/sh\necho "$1" >> "%s/ctl/alerts"\n' "$td" > "$td/alert"
  printf '#!/bin/sh\necho reindex >> "%s/ctl/reindex"\n' "$td" > "$td/reindex"
  chmod +x "$td/bin/"* "$td/alert" "$td/reindex"
}

run_job() {
  rc=0
  PATH="$td/bin:$PATH" FAKE_DOCKER_DIR="$td/ctl" TS_BASE="$td/base" TS_ALERT="$td/alert" \
    TS_REINDEX="$td/reindex" TS_LLM_ENV="$td/llm.env" sh "$RUN_SH" || rc=$?
}

tstart 1
setup
echo 'apply run=true works=10 title_written=2 display_written=0' > "$td/ctl/apply.out"
run_job
[ "$rc" = 0 ] || fail "exit $rc"
[ "$(tr '\n' ' ' < "$td/ctl/lanes")" = "titles release-titles title-review apply intros char-intros " ] || fail "lanes: $(tr '\n' ' ' < "$td/ctl/lanes")"
[ -f "$td/ctl/reindex" ] || fail "no reindex after a shelf move"
[ -f "$td/base/state/last-success" ] || fail "no stamp"
[ ! -f "$td/ctl/alerts" ] || fail "alerted on success"
grep -q -- "--env-file $td/llm.env" "$td/ctl/docker.args" || fail "lanes do not use the gateway env"
! grep -qi cursor "$td/ctl/docker.args" || fail "a lane still reaches Cursor directly"
for scene in catalog.text-safety.title catalog.text-safety.review catalog.text-safety.intro; do
  grep -q -- "-model $scene" "$td/ctl/docker.args" || fail "no $scene"
done
[ ! -f "$td/base/env.tmp" ] || fail "env.tmp left behind"
tend

tstart 2
setup
echo 'apply run=true works=10 title_written=0 display_written=3' > "$td/ctl/apply.out"
echo 1 > "$td/ctl/rc/intros"
run_job
[ "$rc" != 0 ] || fail "a failed lane exited 0"
grep -qx char-intros "$td/ctl/lanes" || fail "char-intros skipped after intros failed"
[ -f "$td/ctl/reindex" ] || fail "reindex skipped after intros failed"
[ ! -f "$td/base/state/last-success" ] || fail "stamped a failed run"
grep -q FAIL "$td/ctl/alerts" 2>/dev/null || fail "no alert"
grep -q 'failed lane(s): intros' "$td/base/logs/"run-*.log || fail "log does not name the failed lane"
tend

tstart 3
setup
echo 'apply run=true works=10 title_written=0 display_written=0' > "$td/ctl/apply.out"
echo 1 > "$td/ctl/rc/titles"
run_job
grep -qx apply "$td/ctl/lanes" || fail "apply skipped after titles failed"
[ ! -f "$td/ctl/reindex" ] || fail "reindexed with no shelf move"
tend

exit "$FAILED"
