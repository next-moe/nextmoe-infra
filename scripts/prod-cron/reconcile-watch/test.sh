#!/bin/sh
# Offline harness for run.sh beside it: fake docker / flock / shred / alert on
# PATH, so it never reaches Docker, the network or a database. Run it on a dev
# box before redeploying run.sh — this directory has no CI.
#
#   sh scripts/prod-cron/reconcile-watch/test.sh
set -eu

RUN_SH="$(cd "$(dirname "$0")" && pwd)/run.sh"
FAILED=0

if [ ! -f "$RUN_SH" ]; then
  echo "FATAL: run.sh not at $RUN_SH" >&2
  exit 1
fi

tstart() {
  T_ID=$1
  T_OK=1
  T_MSG=""
}

fail() {
  T_OK=0
  T_MSG="$T_MSG $*"
}

tend() {
  if [ "$T_OK" = 1 ]; then
    echo "T$T_ID PASS"
  else
    echo "T$T_ID FAIL:$T_MSG"
    FAILED=1
  fi
}

install_fakes() {
  td=$1
  mkdir -p "$td/bin" "$td/sentinel" "$td/base" "$td/lib" "$td/ctl/out" "$td/ctl/rc" "$td/ctl/psql"

  cat > "$td/bin/docker" <<'FAKE'
#!/bin/sh
set -eu
CTL=${FAKE_DOCKER_DIR:?FAKE_DOCKER_DIR unset — refusing to reach a real docker}

{
  echo "###"
  printf '%s\n' "$@"
  echo "###end"
} >> "$CTL/docker.args"

extract_tool_cmd() {
  s=$1
  for t in \
    import-eg-dlsite-releases \
    import-dlsite-games \
    expand-bgm-type4-gated \
    reconcile-eg-anchors \
    reconcile-eg-works \
    reconcile-getchu
  do
    case "$s" in
      *"$t"*)
        printf '%s\n' "$s" | sed "s/.*\($t.*\)/\1/"
        return 0
        ;;
    esac
  done
  return 1
}

join_args() {
  out=""
  for a in "$@"; do
    if [ -z "$out" ]; then
      out=$a
    else
      out="$out $a"
    fi
  done
  printf '%s\n' "$out"
}

emit_tool() {
  key=$1
  if [ -f "$CTL/out/$key" ]; then
    cat "$CTL/out/$key"
    return
  fi
  case "$key" in
    reconcile-eg-anchors)
      echo '2026/09/18 02:00:00 INFO eganchors summary games=0 anchored_games=0 no_evidence_games=0 multi_games=0 twin_games=0 candidate_games=0 rejected_skips=0 exact_planned=0 probable_planned=0 related_planned=0 corroborated=0 written=0 exists=0 errors=0'
      ;;
    reconcile-eg-works)
      echo '2026/09/18 02:00:00 INFO eg-works summary attached=0 quarantined=0 minted_live=0 limited=0 errors=0'
      ;;
    reconcile-getchu)
      echo '2026/09/18 06:00:00 INFO getchuattach summary population=0 attached=0 jan_vndb=0 jan_eg=0 title_date=0 title_cut=0 eg_brand=0 eg_near=0 jan_conflict=0 bundles=0 goods=0 all_ages=0 extras=0 addons=0 reissues=0 cancelled=0 undated=0 brand_unknown=0 unmapped_relations=0 eg_editions=0 rejected_skips=0 mint_groups=0 minted_live=0 minted_quarantined=0 candidates=0 written=0 errors=0'
      ;;
    import-eg-dlsite-releases)
      echo '2026/09/17 05:21:23 INFO eg-dlsite wave summary attached=0 minted=0 already=0 ambiguous=0 missing=0 title_collisions=0 quarantined=0 skipped_intra_collision=0 errors=0'
      ;;
    import-dlsite-games)
      echo '2026/09/18 02:00:00 INFO dlsite-games summary edition_groups=0 declared_groups=0 title_attached_groups=0 minted_groups=0 limited_groups=0 errors=0'
      ;;
    expand-bgm-type4-gated)
      echo '2026/09/17 05:21:17 INFO bgm-type4-gated survey dry=true pool_total=0 to_create=0 to_quarantine=0'
      echo '2026/09/17 05:21:17 INFO bgm-type4-gated summary pool_total=0 gated_total=0 title_collisions=0 to_create=0 to_quarantine=0 works_created=0'
      ;;
    *)
      echo "fake-ok $key"
      ;;
  esac
}

classify_sql() {
  s=$1
  case "$s" in
    *catalog_merge_proposal*)
      printf '%s\n' approved_stale
      return 0
      ;;
    *catalog_match_candidate*)
      case "$s" in
        *status\ =\ 4*|*status=4*)
          printf '%s\n' needs_manual
          return 0
          ;;
        *status\ =\ 0*|*status=0*)
          printf '%s\n' pending_stale
          return 0
          ;;
      esac
      ;;
    *catalog_work*)
      case "$s" in
        *count*)
          printf '%s\n' quarantined_works
          return 0
          ;;
        *)
          printf '%s\n' quarantine_oldest_days
          return 0
          ;;
      esac
      ;;
  esac
  return 1
}

case "$1" in
  pull)
    exit 0
    ;;
  image)
    if [ "${2:-}" = "inspect" ]; then
      echo "ghcr.io/next-moe/infra-tools@sha256:cafebabe00000000000000000000000000000000000000000000000000000000"
      exit 0
    fi
    echo "fake docker: unexpected image subcommand: $*" >&2
    exit 99
    ;;
  inspect)
    printf '%s\n' \
      'KUN_PG_USER=cron' \
      'KUN_PG_PASSWORD=not-on-argv' \
      'KUN_CATALOG_PG_USER=cron' \
      'KUN_CATALOG_PG_PASSWORD=not-on-argv' \
      'KUN_CATALOG_PG_DATABASE=kun_catalog'
    exit 0
    ;;
  exec)
    shift
    while [ $# -gt 0 ]; do
      case "$1" in
        -i|-t|-it|-ti)
          shift
          ;;
        --)
          shift
          break
          ;;
        -*)
          echo "fake docker exec: unknown option: $1" >&2
          exit 99
          ;;
        *)
          break
          ;;
      esac
    done
    if [ $# -lt 1 ]; then
      echo "fake docker exec: missing container" >&2
      exit 99
    fi
    shift
    sql=$(cat)
    if ! key=$(classify_sql "$sql"); then
      echo "fake docker exec: unclassified SQL: $sql" >&2
      exit 99
    fi
    printf '%s\n' "$key" >> "$CTL/psql.log"
    printf '%s\n%s\n' "$key" "$sql" >> "$CTL/psql.sql"
    if [ -f "$CTL/psql/$key" ]; then
      cat "$CTL/psql/$key"
    else
      echo 0
    fi
    rc=0
    if [ -f "$CTL/rc/psql-$key" ]; then
      rc=$(cat "$CTL/rc/psql-$key")
    fi
    exit "$rc"
    ;;
  run)
    shift
    while [ $# -gt 0 ]; do
      case "$1" in
        --rm)
          shift
          ;;
        --network|--env-file|-e|-v|--user|--name)
          if [ $# -lt 2 ]; then
            echo "fake docker run: option $1 needs an argument" >&2
            exit 99
          fi
          shift 2
          ;;
        --network=*|--env-file=*|--user=*|--name=*|-e=*|-v=*)
          shift
          ;;
        --)
          shift
          break
          ;;
        -*)
          echo "fake docker run: unknown option: $1" >&2
          exit 99
          ;;
        *)
          break
          ;;
      esac
    done
    if [ $# -lt 1 ]; then
      echo "fake docker run: missing image" >&2
      exit 99
    fi
    shift

    if [ $# -ge 2 ] && [ "$1" = "sh" ] && [ "$2" = "-c" ]; then
      if [ $# -lt 3 ]; then
        echo "fake docker run: sh -c missing script" >&2
        exit 99
      fi
      if ! toolcmd=$(extract_tool_cmd "$3"); then
        echo "fake docker run: no known tool in sh -c script" >&2
        exit 99
      fi
    else
      toolcmd=$(join_args "$@")
    fi

    tool=""
    for t in \
      import-eg-dlsite-releases \
      import-dlsite-games \
      expand-bgm-type4-gated \
      reconcile-eg-anchors \
      reconcile-eg-works \
      reconcile-getchu
    do
      case "$toolcmd" in
        *"$t"*) tool=$t; break ;;
      esac
    done
    if [ -z "$tool" ]; then
      echo "fake docker run: cannot identify tool: $toolcmd" >&2
      exit 99
    fi

    printf '%s\n' "$tool" >> "$CTL/tools.log"
    emit_tool "$tool"
    rc=0
    if [ -f "$CTL/rc/$tool" ]; then
      rc=$(cat "$CTL/rc/$tool")
    fi
    exit "$rc"
    ;;
  *)
    echo "fake docker: unexpected argv: $*" >&2
    exit 99
    ;;
esac
FAKE

  cat > "$td/bin/flock" <<'FAKE'
#!/bin/sh
set -eu
CTL=${FAKE_DOCKER_DIR:?FAKE_DOCKER_DIR unset — refusing to reach a real flock}
{
  echo "###"
  printf '%s\n' "$@"
  echo "###end"
} >> "$CTL/flock.args"

if [ "$1" = "-n" ]; then
  if [ "${FLOCK_HOLD_FAIL:-0}" = 1 ]; then
    exit 1
  fi
  exit 0
fi

echo "fake flock: unexpected argv: $*" >&2
exit 99
FAKE

  cat > "$td/bin/shred" <<'FAKE'
#!/bin/sh
set -eu
file=""
for a in "$@"; do
  case "$a" in
    -*) ;;
    *) file=$a ;;
  esac
done
if [ -n "$file" ]; then
  rm -f "$file"
fi
FAKE

  cat > "$td/lib/alert.sh" <<'FAKE'
#!/bin/sh
set -eu
dir=$(dirname "$0")
printf '%s\n' "${1:-}" >> "$dir/alert.out"
FAKE

  cat > "$td/sentinel/docker" <<'FAKE'
#!/bin/sh
echo "REAL_DOCKER_REACHED: $*" >&2
exit 99
FAKE

  cat > "$td/sentinel/flock" <<'FAKE'
#!/bin/sh
echo "REAL_FLOCK_REACHED: $*" >&2
exit 99
FAKE

  chmod +x "$td/bin/docker" "$td/bin/flock" "$td/bin/shred" \
    "$td/lib/alert.sh" "$td/sentinel/docker" "$td/sentinel/flock"
}

run_job() {
  td=$1
  export RECONCILE_WATCH_BASE="$td/base"
  export RECONCILE_WATCH_ALERT="$td/lib/alert.sh"
  export FAKE_DOCKER_DIR="$td/ctl"
  export PATH="$td/bin:$td/sentinel:$PATH"

  d=$(command -v docker)
  case "$d" in
    "$td/bin/docker") ;;
    *)
      echo "FATAL: docker resolves to $d, not the fake at $td/bin/docker" >&2
      echo 99 > "$td/ctl/exit"
      return 99
      ;;
  esac
  f=$(command -v flock)
  case "$f" in
    "$td/bin/flock") ;;
    *)
      echo "FATAL: flock resolves to $f, not the fake" >&2
      echo 99 > "$td/ctl/exit"
      return 99
      ;;
  esac

  job_ec=0
  sh "$RUN_SH" || job_ec=$?
  echo "$job_ec" > "$td/ctl/exit"
}

job_exit() {
  cat "$1/ctl/exit"
}

has_stamp() {
  [ -f "$1/base/state/last-success" ]
}

has_alert() {
  [ -s "$1/lib/alert.out" ]
}

job_log() {
  cat "$1"/base/logs/run-*.log 2>/dev/null || true
}

alert_text() {
  if [ -f "$1/lib/alert.out" ]; then
    cat "$1/lib/alert.out"
  fi
}

alert_count() {
  if [ -f "$1/lib/alert.out" ]; then
    grep -c . "$1/lib/alert.out"
  else
    echo 0
  fi
}

expect_exit() {
  td=$1
  want=$2
  got=$(job_exit "$td")
  if [ "$got" != "$want" ]; then
    fail "exit $got want $want"
  fi
}

expect_exit_nonzero() {
  td=$1
  got=$(job_exit "$td")
  if [ "$got" = "0" ]; then
    fail "exit 0 want non-zero"
  fi
}

scan_no_apply() {
  argsfile=$1
  if [ ! -f "$argsfile" ]; then
    fail "no recorded docker argv"
    return 0
  fi
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      *"--apply"*|*"--run"*|*"-run"*)
        fail "apply/run flag in docker argv: $line"
        ;;
    esac
  done < "$argsfile"
}

# --- T1 ---
tstart 1
td=$(mktemp -d)
install_fakes "$td"
run_job "$td"
expect_exit "$td" 0
if ! has_stamp "$td"; then fail "missing stamp"; fi
if has_alert "$td"; then fail "unexpected alert"; fi
if ! job_log "$td" | grep -q 'recon verdict=converged'; then fail "missing converged verdict"; fi
tend
rm -rf "$td"

# --- T2 ---
tstart 2
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/18 02:00:00 INFO eganchors summary exact_planned=3 probable_planned=0 related_planned=0' \
  > "$td/ctl/out/reconcile-eg-anchors"
run_job "$td"
expect_exit "$td" 0
if ! has_stamp "$td"; then fail "missing stamp"; fi
if [ "$(alert_count "$td")" != 1 ]; then fail "alert count $(alert_count "$td") want 1"; fi
if ! alert_text "$td" | grep -q '\[RECON\]'; then fail "no RECON alert"; fi
if ! alert_text "$td" | grep -q 'eg-anchors'; then fail "alert did not name eg-anchors"; fi
tend
rm -rf "$td"

# --- T3 ---
tstart 3
td=$(mktemp -d)
install_fakes "$td"
echo 5 > "$td/ctl/psql/needs_manual"
run_job "$td"
expect_exit "$td" 0
if ! has_stamp "$td"; then fail "missing stamp"; fi
if ! alert_text "$td" | grep -q '\[RECON\]'; then fail "no RECON alert"; fi
if ! alert_text "$td" | grep -q 'needs_manual'; then fail "alert did not name needs_manual"; fi
tend
rm -rf "$td"

# --- T4 ---
tstart 4
td=$(mktemp -d)
install_fakes "$td"
echo 15 > "$td/ctl/psql/quarantine_oldest_days"
run_job "$td"
expect_exit "$td" 0
if ! has_stamp "$td"; then fail "missing stamp"; fi
if ! alert_text "$td" | grep -q '\[RECON\]'; then fail "no RECON alert"; fi
if ! alert_text "$td" | grep -q 'quarantine_oldest_days'; then fail "alert did not name quarantine_oldest_days"; fi
tend
rm -rf "$td"

# --- T5 ---
tstart 5
td=$(mktemp -d)
install_fakes "$td"
echo 1 > "$td/ctl/rc/reconcile-eg-anchors"
run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! alert_text "$td" | grep -q '\[FAIL\]'; then fail "no FAIL alert"; fi
tend
rm -rf "$td"

# --- T6 ---
tstart 6
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/18 02:00:00 INFO eganchors summary exact_planned=0 probable_planned=0' \
  > "$td/ctl/out/reconcile-eg-anchors"
run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! alert_text "$td" | grep -q '\[FAIL\]'; then fail "no FAIL alert"; fi
tend
rm -rf "$td"

# --- T7 ---
tstart 7
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/18 02:00:00 INFO eganchors summary exact_planned=2 probable_planned=0 related_planned=0' \
  > "$td/ctl/out/reconcile-eg-anchors"
echo 5 > "$td/ctl/psql/needs_manual"
run_job "$td"
expect_exit "$td" 0
if ! has_stamp "$td"; then fail "missing stamp"; fi
if [ "$(alert_count "$td")" != 1 ]; then fail "alert count $(alert_count "$td") want 1"; fi
if ! alert_text "$td" | grep -q '\[RECON\]'; then fail "no RECON alert"; fi
if ! alert_text "$td" | grep -q 'eg-anchors'; then fail "alert did not name eg-anchors"; fi
if ! alert_text "$td" | grep -q 'needs_manual'; then fail "alert did not name needs_manual"; fi
tend
rm -rf "$td"

# --- T8 ---
tstart 8
td=$(mktemp -d)
install_fakes "$td"
run_job "$td"
expect_exit "$td" 0
scan_no_apply "$td/ctl/docker.args"
tend
rm -rf "$td"

# --- T9 ---
tstart 9
td=$(mktemp -d)
install_fakes "$td"
echo 5 > "$td/ctl/psql/needs_manual"
NEEDS_MANUAL_MAX=10 run_job "$td"
expect_exit "$td" 0
if ! has_stamp "$td"; then fail "missing stamp"; fi
if has_alert "$td"; then fail "unexpected alert"; fi
if ! job_log "$td" | grep -q 'recon verdict=converged'; then fail "missing converged verdict"; fi
tend
rm -rf "$td"

# --- T10 ---
tstart 10
td=$(mktemp -d)
install_fakes "$td"
FLOCK_HOLD_FAIL=1 run_job "$td"
expect_exit "$td" 0
if has_stamp "$td"; then fail "stamp written"; fi
if has_alert "$td"; then fail "unexpected alert"; fi
tend
rm -rf "$td"

if [ "$FAILED" -ne 0 ]; then
  exit 1
fi
exit 0
