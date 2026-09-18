#!/bin/sh
# Offline harness for run.sh beside it: fake docker / flock / shred / alert /
# reindex on PATH, so it never reaches Docker, the network or a database.
#
#   sh scripts/prod-cron/llm-adjudicate-nightly/test.sh
set -eu

if [ -z "${RUN_SH:-}" ]; then
  RUN_SH="$(cd "$(dirname "$0")" && pwd)/run.sh"
fi
FAILED=0
SENTINEL_KEY="sentinel-key-do-not-print"

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

write_t1_names() {
  cat > "$1" <<'EOF'
adj-judge-workpair
adj-apply-workpair
adj-approve
adj-execute
adj-release
adj-judge-creditname
adj-apply-creditname
adj-judge-refs
adj-apply-refs
EOF
}

install_fakes() {
  td=$1
  mkdir -p "$td/bin" "$td/sentinel" "$td/base" "$td/lib" "$td/ctl/out" "$td/ctl/rc"
  printf '%s\n' "KUN_LLM_API_KEY=$SENTINEL_KEY" > "$td/llm.env"

  cat > "$td/bin/docker" <<'FAKE'
#!/bin/sh
set -eu
CTL=${FAKE_DOCKER_DIR:?FAKE_DOCKER_DIR unset — refusing to reach a real docker}

{
  echo "###"
  printf '%s\n' "$@"
  echo "###end"
} >> "$CTL/docker.args"

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
  run)
    shift
    name=""
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
          if [ "$1" = "--name" ]; then
            name=$2
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
    if [ -z "$name" ]; then
      echo "fake docker run: missing --name" >&2
      exit 99
    fi
    case "$name" in
      adj-judge-workpair|adj-apply-workpair|adj-approve|adj-execute|adj-release|adj-judge-creditname|adj-apply-creditname|adj-judge-refs|adj-apply-refs)
        ;;
      *)
        echo "fake docker run: unexpected --name $name" >&2
        exit 99
        ;;
    esac
    printf '%s\n' "$name" >> "$CTL/seq.log"

    if [ -f "$CTL/out/$name" ]; then
      cat "$CTL/out/$name"
    else
      case "$name" in
        adj-judge-workpair)
          echo "2026/09/17 10:52:40 INFO queue-workpair done judged=10 errors=0 dry=false"
          ;;
        adj-apply-workpair)
          echo "apply workpair ok"
          ;;
        adj-approve)
          echo "[approve] note=llm:queue-adjudicator open=3 approved=3 skipped=0"
          ;;
        adj-execute)
          echo "[execute] cooled=2 executed=2 chain_superseded=0 errors=0"
          ;;
        adj-release)
          echo "[release] quarantined=0 held=0 released=0"
          ;;
        adj-judge-creditname)
          echo "2026/09/17 10:52:40 INFO queue-creditname done judged=20 errors=0 dry=false"
          ;;
        adj-apply-creditname)
          echo "apply creditname ok"
          ;;
        adj-judge-refs)
          echo "2026/09/17 10:52:40 INFO queue-refs done judged=5 errors=0 dry=false"
          ;;
        adj-apply-refs)
          echo "apply refs ok"
          ;;
      esac
    fi

    rc=0
    if [ -f "$CTL/rc/$name" ]; then
      rc=$(cat "$CTL/rc/$name")
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
  if [ -f "$CTL/flock_n_fail" ]; then
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

  cat > "$td/lib/reindex.sh" <<'FAKE'
#!/bin/sh
set -eu
CTL=${FAKE_DOCKER_DIR:?FAKE_DOCKER_DIR unset}
echo reindex >> "$CTL/seq.log"
echo called >> "$CTL/reindex.log"
if [ -f "$CTL/rc/reindex" ]; then
  exit "$(cat "$CTL/rc/reindex")"
fi
exit 0
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
    "$td/lib/alert.sh" "$td/lib/reindex.sh" \
    "$td/sentinel/docker" "$td/sentinel/flock"
}

run_job() {
  td=$1
  export ADJ_BASE="$td/base"
  export ADJ_ALERT="$td/lib/alert.sh"
  export ADJ_REINDEX="$td/lib/reindex.sh"
  export ADJ_LLM_ENV="$td/llm.env"
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

alert_text() {
  if [ -f "$1/lib/alert.out" ]; then
    tr '\n' ' ' < "$1/lib/alert.out"
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

expect_stamp() {
  if ! has_stamp "$1"; then
    fail "missing stamp"
  fi
}

expect_no_stamp() {
  if has_stamp "$1"; then
    fail "stamp written"
  fi
}

expect_no_alert() {
  if has_alert "$1"; then
    fail "unexpected alert: $(alert_text "$1")"
  fi
}

expect_fail_alert() {
  if [ ! -f "$1/lib/alert.out" ]; then
    fail "no alert"
    return 0
  fi
  if ! grep -q '\[FAIL\] llm adjudicate nightly' "$1/lib/alert.out"; then
    fail "no [FAIL] alert: $(alert_text "$1")"
  fi
}

expect_env_gone() {
  if [ -f "$1/base/env.tmp" ]; then
    fail "env.tmp remains"
  fi
}

expect_reindex() {
  td=$1
  want=$2
  got=0
  if [ -f "$td/ctl/reindex.log" ]; then
    got=$(wc -l < "$td/ctl/reindex.log")
    got=$((got + 0))
  fi
  if [ "$got" -ne "$want" ]; then
    fail "reindex $got want $want"
  fi
}

expect_no_docker() {
  if [ -f "$1/ctl/docker.args" ]; then
    fail "docker ran"
  fi
}

collect_names() {
  argsfile="$1/ctl/docker.args"
  out="$1/ctl/names.got"
  : > "$out"
  [ -f "$argsfile" ] || return 0
  isrun=0
  first=0
  name=""
  prev=""
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      "###")
        isrun=0
        first=1
        name=""
        prev=""
        continue
        ;;
      "###end")
        if [ "$isrun" = 1 ] && [ -n "$name" ]; then
          printf '%s\n' "$name" >> "$out"
        fi
        isrun=0
        first=0
        continue
        ;;
    esac
    if [ "$first" = 1 ]; then
      first=0
      if [ "$line" = "run" ]; then
        isrun=1
      fi
    fi
    if [ "$prev" = "--name" ]; then
      name=$line
    fi
    prev=$line
  done < "$argsfile"
}

has_name() {
  td=$1
  l=$2
  [ -f "$td/ctl/names.got" ] && grep -qx "$l" "$td/ctl/names.got"
}

expect_seq() {
  td=$1
  exp=$2
  collect_names "$td"
  if ! diff -u "$exp" "$td/ctl/names.got"; then
    fail "docker-run sequence mismatch"
  fi
}

expect_no_name() {
  td=$1
  l=$2
  collect_names "$td"
  if has_name "$td" "$l"; then
    fail "$l ran"
  fi
}

expect_name() {
  td=$1
  l=$2
  collect_names "$td"
  if ! has_name "$td" "$l"; then
    fail "$l missing"
  fi
}

c_arg_for() {
  td=$1
  want=$2
  argsfile="$td/ctl/docker.args"
  [ -f "$argsfile" ] || return 0
  isrun=0
  first=0
  name=""
  carg=""
  prev=""
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      "###")
        isrun=0
        first=1
        name=""
        carg=""
        prev=""
        continue
        ;;
      "###end")
        if [ "$isrun" = 1 ] && [ "$name" = "$want" ]; then
          printf '%s\n' "$carg"
          return 0
        fi
        continue
        ;;
    esac
    if [ "$first" = 1 ]; then
      first=0
      if [ "$line" = "run" ]; then
        isrun=1
      fi
    fi
    if [ "$prev" = "--name" ]; then
      name=$line
    fi
    if [ "$prev" = "-c" ]; then
      carg=$line
    fi
    prev=$line
  done < "$argsfile"
}

expect_c_contains() {
  td=$1
  name=$2
  needle=$3
  c=$(c_arg_for "$td" "$name")
  if [ -z "$c" ]; then
    fail "$name: empty command string"
    return 0
  fi
  case "$c" in
    *"$needle"*) ;;
    *) fail "$name cmd missing [$needle]: $c" ;;
  esac
}

expect_c_lacks() {
  td=$1
  name=$2
  needle=$3
  c=$(c_arg_for "$td" "$name")
  case "$c" in
    *"$needle"*) fail "$name cmd must not carry [$needle]: $c" ;;
  esac
}

expect_reindex_between() {
  td=$1
  seq="$td/ctl/seq.log"
  if [ ! -f "$seq" ]; then
    fail "no seq.log"
    return 0
  fi
  n=$(grep -c '^reindex$' "$seq" || true)
  n=$((n + 0))
  if [ "$n" -ne 1 ]; then
    fail "reindex count $n want 1"
    return 0
  fi
  prev=""
  saw=0
  bad=0
  while IFS= read -r line || [ -n "$line" ]; do
    if [ "$line" = "reindex" ]; then
      if [ "$prev" != "adj-release" ]; then
        bad=1
      fi
      saw=1
    elif [ "$saw" = 1 ]; then
      if [ "$line" != "adj-judge-creditname" ]; then
        bad=1
      fi
      saw=2
    fi
    prev=$line
  done < "$seq"
  if [ "$bad" = 1 ] || [ "$saw" != 2 ]; then
    fail "reindex not between adj-release and adj-judge-creditname"
  fi
}

scan_t3() {
  td=$1
  argsfile="$td/ctl/docker.args"
  llm="$td/llm.env"
  envtmp="$td/base/env.tmp"
  if [ ! -f "$argsfile" ]; then
    fail "no recorded docker argv"
    return 0
  fi
  njudge=0
  napply=0
  isrun=0
  first=0
  is_judge=0
  is_apply=0
  has_llm=0
  has_envtmp=0
  name=""
  prev=""
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      *"$SENTINEL_KEY"*)
        fail "argv leak: $line"
        ;;
    esac
    case "$line" in
      "###")
        isrun=0
        first=1
        is_judge=0
        is_apply=0
        has_llm=0
        has_envtmp=0
        name=""
        prev=""
        continue
        ;;
      "###end")
        if [ "$isrun" = 1 ]; then
          if [ "$is_judge" = 1 ]; then
            njudge=$((njudge + 1))
            if [ "$has_llm" != 1 ]; then
              fail "judge $name missing --env-file LLM_ENV"
            fi
            if [ "$has_envtmp" != 1 ]; then
              fail "judge $name missing env.tmp"
            fi
          elif [ "$is_apply" = 1 ]; then
            napply=$((napply + 1))
            if [ "$has_envtmp" != 1 ]; then
              fail "$name missing env.tmp"
            fi
            if [ "$has_llm" = 1 ]; then
              fail "$name carried LLM env"
            fi
          fi
        fi
        isrun=0
        first=0
        continue
        ;;
    esac
    if [ "$first" = 1 ]; then
      first=0
      if [ "$line" = "run" ]; then
        isrun=1
      fi
    fi
    if [ "$prev" = "--env-file" ]; then
      if [ "$line" = "$llm" ]; then
        has_llm=1
      fi
      if [ "$line" = "$envtmp" ]; then
        has_envtmp=1
      fi
    fi
    if [ "$prev" = "--name" ]; then
      name=$line
      case "$line" in
        adj-judge-*) is_judge=1 ;;
        adj-apply-*|adj-approve|adj-execute|adj-release) is_apply=1 ;;
      esac
    fi
    prev=$line
  done < "$argsfile"
  if [ "$njudge" -ne 3 ]; then
    fail "judge docker runs=$njudge want 3"
  fi
  if [ "$napply" -ne 6 ]; then
    fail "apply/approve/execute docker runs=$napply want 6"
  fi
}

# --- T1 ---
tstart 1
td=$(mktemp -d)
install_fakes "$td"
run_job "$td"
expect_exit "$td" 0
write_t1_names "$td/ctl/expected"
expect_seq "$td" "$td/ctl/expected"
expect_reindex "$td" 1
expect_reindex_between "$td"
expect_stamp "$td"
expect_env_gone "$td"
expect_no_alert "$td"
tend
rm -rf "$td"

# --- T2 ---
tstart 2
td=$(mktemp -d)
install_fakes "$td"
run_job "$td"
expect_exit "$td" 0
expect_c_contains "$td" adj-judge-creditname "--apply --task queue-creditname --limit 300"
expect_c_contains "$td" adj-apply-creditname "--mode apply --queue creditname --actor 1 --min-confidence 0.9 --min-confidence-reject 0.8 --apply"
expect_c_lacks "$td" adj-apply-creditname "--limit"
expect_c_contains "$td" adj-apply-refs "--min-confidence-reject 0.8"
expect_c_contains "$td" adj-release "work-dedup -mode release -actor 1 -note"
expect_c_contains "$td" adj-release "-run"
tend
rm -rf "$td"

# --- T3 ---
tstart 3
td=$(mktemp -d)
install_fakes "$td"
run_job "$td"
expect_exit "$td" 0
scan_t3 "$td"
tend
rm -rf "$td"

# --- T4 ---
tstart 4
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/17 10:52:40 INFO queue-creditname done judged=10 errors=2 dry=false' \
  > "$td/ctl/out/adj-judge-creditname"
echo 1 > "$td/ctl/rc/adj-judge-creditname"
run_job "$td"
expect_exit "$td" 0
expect_name "$td" adj-apply-refs
expect_stamp "$td"
expect_no_alert "$td"
tend
rm -rf "$td"

# --- T5 ---
tstart 5
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/17 10:52:40 INFO queue-creditname done judged=1 errors=40 dry=false' \
  > "$td/ctl/out/adj-judge-creditname"
echo 1 > "$td/ctl/rc/adj-judge-creditname"
run_job "$td"
expect_exit "$td" 0
if [ ! -f "$td/lib/alert.out" ]; then
  fail "no alert"
else
  n=$(grep -c '\[ADJ\] queue-creditname:' "$td/lib/alert.out" || true)
  n=$((n + 0))
  if [ "$n" -ne 1 ]; then
    fail "[ADJ] queue-creditname: count $n: $(alert_text "$td")"
  fi
  if grep -q '\[FAIL\]' "$td/lib/alert.out"; then
    fail "unexpected [FAIL] with [ADJ]: $(alert_text "$td")"
  fi
fi
expect_name "$td" adj-apply-refs
expect_stamp "$td"
tend
rm -rf "$td"

# --- T6 ---
tstart 6
td=$(mktemp -d)
install_fakes "$td"
: > "$td/ctl/out/adj-judge-creditname"
echo 1 > "$td/ctl/rc/adj-judge-creditname"
run_job "$td"
expect_exit "$td" 1
expect_fail_alert "$td"
expect_no_name "$td" adj-apply-creditname
expect_no_name "$td" adj-judge-refs
expect_no_name "$td" adj-apply-refs
expect_no_stamp "$td"
expect_env_gone "$td"
tend
rm -rf "$td"

# --- T7: a stale same-day done line must not count ---
tstart 7
td=$(mktemp -d)
install_fakes "$td"
mkdir -p "$td/base/logs"
printf '%s\n' '2026/09/17 10:52:40 INFO queue-creditname done judged=5 errors=0 dry=false' \
  > "$td/base/logs/run-$(date +%F).log"
: > "$td/ctl/out/adj-judge-creditname"
echo 1 > "$td/ctl/rc/adj-judge-creditname"
run_job "$td"
expect_exit "$td" 1
expect_fail_alert "$td"
expect_no_stamp "$td"
tend
rm -rf "$td"

# --- T8: a done line from a different task does not count ---
tstart 8
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/17 10:52:40 INFO queue-workpair done judged=10 errors=0' \
  > "$td/ctl/out/adj-judge-workpair"
printf '%s\n' '2026/09/17 10:52:40 INFO queue-workpair done judged=10 errors=0' \
  > "$td/ctl/out/adj-judge-creditname"
echo 1 > "$td/ctl/rc/adj-judge-creditname"
run_job "$td"
expect_exit "$td" 1
expect_fail_alert "$td"
tend
rm -rf "$td"

# --- T9 ---
tstart 9
td=$(mktemp -d)
install_fakes "$td"
echo 1 > "$td/ctl/rc/adj-apply-creditname"
run_job "$td"
expect_exit "$td" 1
expect_fail_alert "$td"
expect_no_name "$td" adj-judge-refs
expect_no_name "$td" adj-apply-refs
expect_no_stamp "$td"
tend
rm -rf "$td"

# --- T10 ---
tstart 10
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '[approve] note=llm:queue-adjudicator open=200 approved=200 skipped=0' \
  > "$td/ctl/out/adj-approve"
run_job "$td"
if [ ! -f "$td/lib/alert.out" ]; then
  fail "no alert"
else
  n=$(grep -c '\[ADJ\] llm adjudicate backlog' "$td/lib/alert.out" || true)
  n=$((n + 0))
  if [ "$n" -ne 1 ]; then
    fail "[ADJ] backlog count $n: $(alert_text "$td")"
  fi
  if grep -q '\[FAIL\]' "$td/lib/alert.out"; then
    fail "unexpected [FAIL] with backlog: $(alert_text "$td")"
  fi
fi
expect_name "$td" adj-apply-refs
expect_exit "$td" 0
expect_stamp "$td"
tend
rm -rf "$td"

# --- T11 ---
tstart 11
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '[execute] cooled=0 executed=0 chain_superseded=0 errors=0' \
  > "$td/ctl/out/adj-execute"
run_job "$td"
expect_reindex "$td" 0
expect_exit "$td" 0
tend
rm -rf "$td"

# --- T12 ---
tstart 12
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '[execute] cooled=1 executed=1 chain_superseded=0 errors=0' \
  > "$td/ctl/out/adj-execute"
echo 1 > "$td/ctl/rc/reindex"
run_job "$td"
expect_exit "$td" 0
expect_stamp "$td"
expect_no_alert "$td"
tend
rm -rf "$td"

# --- T13: a run skipped by the lock must not shred the running run's env snapshot ---
tstart 13
td=$(mktemp -d)
install_fakes "$td"
mkdir -p "$td/base"
echo 'KUN_PG_PASSWORD=held-by-the-running-instance' > "$td/base/env.tmp"
: > "$td/ctl/flock_n_fail"
run_job "$td"
expect_exit "$td" 0
expect_no_alert "$td"
expect_no_stamp "$td"
if [ ! -f "$td/base/env.tmp" ]; then
  fail "lock-skipped run deleted the running run's env.tmp"
fi
expect_no_docker "$td"
tend
rm -rf "$td"

# --- T14 ---
tstart 14
td=$(mktemp -d)
install_fakes "$td"
: > "$td/ctl/out/adj-judge-workpair"
echo 1 > "$td/ctl/rc/adj-judge-workpair"
run_job "$td"
expect_exit "$td" 1
expect_fail_alert "$td"
printf '%s\n' 'adj-judge-workpair' > "$td/ctl/expected"
expect_seq "$td" "$td/ctl/expected"
expect_no_stamp "$td"
tend
rm -rf "$td"

# --- T15 ---
tstart 15
td=$(mktemp -d)
install_fakes "$td"
echo 1 > "$td/ctl/rc/adj-execute"
run_job "$td"
expect_exit "$td" 1
expect_fail_alert "$td"
expect_no_name "$td" adj-judge-creditname
expect_no_name "$td" adj-apply-creditname
expect_no_name "$td" adj-judge-refs
expect_no_name "$td" adj-apply-refs
expect_no_name "$td" adj-release
expect_reindex "$td" 0
expect_no_stamp "$td"
tend
rm -rf "$td"

if [ "$FAILED" -ne 0 ]; then
  exit 1
fi
exit 0
