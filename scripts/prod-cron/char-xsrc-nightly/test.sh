#!/bin/sh
# Offline harness for run.sh beside it: fake docker / flock / shred / alert on
# PATH, so it never reaches Docker, the network or a database. Run it on a dev
# box before redeploying run.sh — this directory has no CI.
#
#   sh scripts/prod-cron/char-xsrc-nightly/test.sh
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

write_t1_expected() {
  cat > "$1" <<'EOF'
packets
adjudicate-r1
panel-packets
adjudicate-panel
emit-all
propose
propose-run
execute-run
EOF
}

install_fakes() {
  td=$1
  mkdir -p "$td/bin" "$td/sentinel" "$td/base" "$td/lib" "$td/ctl/out" "$td/ctl/rc"
  : > "$td/adj.lock"
  : > "$td/mt.lock"
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
    if [ $# -lt 1 ]; then
      echo "fake docker run: missing image" >&2
      exit 99
    fi
    shift

    emit_rc() {
      key=$1
      rc=0
      if [ -f "$CTL/rc/$key" ]; then
        rc=$(cat "$CTL/rc/$key")
      fi
      exit "$rc"
    }

    if [ -n "$name" ]; then
      case "$name" in
        xsrc-judge-r1) key=adjudicate-r1 ;;
        xsrc-judge-panel) key=adjudicate-panel ;;
        *)
          echo "fake docker run: unexpected --name $name" >&2
          exit 99
          ;;
      esac
      printf '%s\n' "$key" >> "$CTL/tools.log"
      if [ -f "$CTL/out/$key" ]; then
        cat "$CTL/out/$key"
      else
        echo "2026/09/17 04:00:00 INFO adjudicate done prompt_version=person-adjudicate-v1 packets=1 skipped_already_judged=0 judged=1 merge=0 distinct=0 unsure=0 errors=0 elapsed=3s"
      fi
      emit_rc "$key"
    fi

    if [ $# -lt 1 ]; then
      echo "fake docker run: missing command" >&2
      exit 99
    fi

    cmd=$1
    mode=""
    runflag=0
    prev=""
    for a in "$@"; do
      if [ "$prev" = "-mode" ]; then
        mode=$a
      fi
      if [ "$a" = "-run" ]; then
        runflag=1
      fi
      prev=$a
    done

    case "$cmd" in
      catalog-char-xsrc)
        case "$mode" in
          packets) key=packets ;;
          panel-packets) key=panel-packets ;;
          emit-all) key=emit-all ;;
          *)
            echo "fake docker run: unknown catalog-char-xsrc -mode $mode" >&2
            exit 99
            ;;
        esac
        printf '%s\n' "$key" >> "$CTL/tools.log"
        if [ "$key" = "emit-all" ]; then
          base=${XSRC_BASE:?XSRC_BASE unset}
          mkdir -p "$base/work"
          if [ -f "$CTL/empty_worklist" ]; then
            : > "$base/work/worklist.jsonl"
          else
            printf '%s\n' '{"id":1}' > "$base/work/worklist.jsonl"
          fi
        fi
        echo "fake-ok $key"
        emit_rc "$key"
        ;;
      catalog-dedup-batch)
        case "$mode" in
          propose)
            if [ "$runflag" = 1 ]; then
              key=propose-run
            else
              key=propose
            fi
            ;;
          execute)
            key=execute-run
            ;;
          *)
            echo "fake docker run: unknown catalog-dedup-batch -mode $mode" >&2
            exit 99
            ;;
        esac
        printf '%s\n' "$key" >> "$CTL/tools.log"
        if [ "$key" = "propose" ] && [ -f "$CTL/propose_silent" ]; then
          echo "catalog-dedup-batch: dry run produced no summary"
          emit_rc "$key"
        fi
        if [ "$key" = "propose" ] || [ "$key" = "propose-run" ]; then
          n=3
          if [ -f "$CTL/propose_count" ]; then
            n=$(cat "$CTL/propose_count")
          fi
          echo "DRY-RUN (pass -run to propose+approve) [propose] worklist=/w/work/worklist.jsonl note=rule:catalog-dedup char-xsrc-nightly groups=1 pairs=1 proposals=$n approved=0 skipped=0 decided=0 errors=0"
          emit_rc "$key"
        fi
        n=2
        if [ -f "$CTL/execute_count" ]; then
          n=$(cat "$CTL/execute_count")
        fi
        echo "APPLIED [execute] cooled=$n executed=$n chain_superseded=0 chain_residual=0 errors=0"
        emit_rc "$key"
        ;;
      sh)
        echo "fake docker run: sh -c without --name xsrc-judge-*" >&2
        exit 99
        ;;
      *)
        echo "fake docker run: cannot identify command: $*" >&2
        exit 99
        ;;
    esac
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

if [ "$1" = "-w" ]; then
  if [ $# -lt 3 ]; then
    echo "fake flock -w: need timeout and path" >&2
    exit 99
  fi
  path=$3
  if [ -f "$CTL/flock_wait_fail" ]; then
    failpath=$(cat "$CTL/flock_wait_fail")
    if [ "$path" = "$failpath" ]; then
      exit 1
    fi
  fi
  shift 2
  shift
  if [ $# -eq 0 ]; then
    exit 0
  fi
  exec "$@"
fi

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
echo called >> "$CTL/reindex.log"
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
  export XSRC_BASE="$td/base"
  export XSRC_ALERT="$td/lib/alert.sh"
  export XSRC_ADJ_LOCK="$td/adj.lock"
  export XSRC_MT_LOCK="$td/mt.lock"
  export XSRC_REINDEX="$td/lib/reindex.sh"
  export XSRC_LLM_ENV="$td/llm.env"
  export FAKE_DOCKER_DIR="$td/ctl"
  export PATH="$td/bin:$td/sentinel:$PATH"
  if [ -n "${XSRC_PROPOSE_MAX:-}" ]; then
    export XSRC_PROPOSE_MAX
  fi
  if [ -n "${XSRC_JUDGE_LIMIT:-}" ]; then
    export XSRC_JUDGE_LIMIT
  fi

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
  if ! grep -q '\[FAIL\] char-xsrc nightly' "$1/lib/alert.out"; then
    fail "no [FAIL] alert: $(alert_text "$1")"
  fi
}

expect_xsrc_alert_only() {
  if [ ! -f "$1/lib/alert.out" ]; then
    fail "no alert"
    return 0
  fi
  n=$(grep -c '\[XSRC\]' "$1/lib/alert.out" || true)
  n=$((n + 0))
  if [ "$n" -ne 1 ]; then
    fail "[XSRC] count $n: $(alert_text "$1")"
  fi
  if grep -q '\[FAIL\]' "$1/lib/alert.out"; then
    fail "unexpected [FAIL] with [XSRC]: $(alert_text "$1")"
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

RL_OUT=""
RL_ISRUN=0
RL_FIRST=0
RL_KIND=""
RL_MODE=""
RL_RUNFLAG=0
RL_NAME=""
RL_PREV=""

reset_rl() {
  RL_ISRUN=0
  RL_FIRST=0
  RL_KIND=""
  RL_MODE=""
  RL_RUNFLAG=0
  RL_NAME=""
  RL_PREV=""
}

flush_label() {
  if [ "$RL_ISRUN" != 1 ]; then
    return 0
  fi
  label=""
  case "$RL_NAME" in
    xsrc-judge-r1) label=adjudicate-r1 ;;
    xsrc-judge-panel) label=adjudicate-panel ;;
  esac
  if [ -z "$label" ]; then
    if [ "$RL_KIND" = "catalog-char-xsrc" ]; then
      case "$RL_MODE" in
        packets) label=packets ;;
        panel-packets) label=panel-packets ;;
        emit-all) label=emit-all ;;
      esac
    elif [ "$RL_KIND" = "catalog-dedup-batch" ]; then
      if [ "$RL_MODE" = "propose" ]; then
        if [ "$RL_RUNFLAG" = 1 ]; then
          label=propose-run
        else
          label=propose
        fi
      elif [ "$RL_MODE" = "execute" ]; then
        label=execute-run
      fi
    fi
  fi
  if [ -n "$label" ]; then
    printf '%s\n' "$label" >> "$RL_OUT"
  fi
}

run_labels() {
  argsfile=$1
  RL_OUT=$2
  : > "$RL_OUT"
  [ -f "$argsfile" ] || return 0
  reset_rl
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      "###")
        reset_rl
        RL_FIRST=1
        continue
        ;;
      "###end")
        flush_label
        reset_rl
        continue
        ;;
    esac
    if [ "$RL_FIRST" = 1 ]; then
      RL_FIRST=0
      if [ "$line" = "run" ]; then
        RL_ISRUN=1
      fi
    fi
    if [ "$RL_PREV" = "--name" ]; then
      RL_NAME=$line
    fi
    if [ "$RL_PREV" = "-mode" ]; then
      RL_MODE=$line
    fi
    if [ "$line" = "-run" ]; then
      RL_RUNFLAG=1
    fi
    if [ "$line" = "catalog-char-xsrc" ]; then
      RL_KIND=catalog-char-xsrc
    fi
    if [ "$line" = "catalog-dedup-batch" ]; then
      RL_KIND=catalog-dedup-batch
    fi
    RL_PREV=$line
  done < "$argsfile"
}

collect_labels() {
  run_labels "$1/ctl/docker.args" "$1/ctl/labels.got"
}

has_label() {
  td=$1
  l=$2
  [ -f "$td/ctl/labels.got" ] && grep -qx "$l" "$td/ctl/labels.got"
}

expect_seq() {
  td=$1
  exp=$2
  collect_labels "$td"
  if ! diff -u "$exp" "$td/ctl/labels.got"; then
    fail "docker-run sequence mismatch"
  fi
}

expect_no_label() {
  td=$1
  l=$2
  if has_label "$td" "$l"; then
    fail "$l ran"
  fi
}

expect_label() {
  td=$1
  l=$2
  if ! has_label "$td" "$l"; then
    fail "$l missing"
  fi
}

scan_t16() {
  td=$1
  n=$(awk '
    $0 == "###" { judge = 0; rt = ""; dl = ""; prev = ""; next }
    $0 == "###end" { if (judge && rt != "" && rt != "0" && dl != "" && dl != "0") ok++; next }
    prev == "--name" && $0 ~ /^xsrc-judge-/ { judge = 1 }
    prev == "-request-timeout" { rt = $0 }
    prev == "-deadline" { dl = $0 }
    { prev = $0 }
    END { print ok + 0 }
  ' "$td/ctl/docker.args")
  if [ "$n" != 2 ]; then
    fail "judges bounded by -request-timeout and -deadline: $n of 2"
  fi
}

scan_t14() {
  td=$1
  argsfile="$td/ctl/docker.args"
  llm="$td/llm.env"
  envtmp="$td/base/env.tmp"
  if [ ! -f "$argsfile" ]; then
    fail "no recorded docker argv"
    return 0
  fi
  njudge=0
  ncat=0
  isrun=0
  first=0
  is_judge=0
  is_cat=0
  has_llm=0
  has_envtmp=0
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
        is_cat=0
        has_llm=0
        has_envtmp=0
        prev=""
        continue
        ;;
      "###end")
        if [ "$isrun" = 1 ]; then
          if [ "$is_judge" = 1 ]; then
            njudge=$((njudge + 1))
            if [ "$has_llm" != 1 ]; then
              fail "judge missing --env-file LLM_ENV"
            fi
            if [ "$has_envtmp" = 1 ]; then
              fail "judge carried env.tmp"
            fi
          elif [ "$is_cat" = 1 ]; then
            ncat=$((ncat + 1))
            if [ "$has_envtmp" != 1 ]; then
              fail "catalog run missing env.tmp"
            fi
            if [ "$has_llm" = 1 ]; then
              fail "catalog run carried LLM env"
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
      case "$line" in
        xsrc-judge-r1|xsrc-judge-panel) is_judge=1 ;;
      esac
    fi
    if [ "$line" = "catalog-char-xsrc" ] || [ "$line" = "catalog-dedup-batch" ]; then
      is_cat=1
    fi
    prev=$line
  done < "$argsfile"
  if [ "$njudge" -ne 2 ]; then
    fail "judge docker runs=$njudge want 2"
  fi
  if [ "$ncat" -ne 6 ]; then
    fail "catalog-touching docker runs=$ncat want 6"
  fi
}

scan_t15() {
  td=$1
  argsfile="$td/ctl/docker.args"
  notes="$td/ctl/notes.got"
  : > "$notes"
  if [ ! -f "$argsfile" ]; then
    fail "no recorded docker argv"
    return 0
  fi
  isrun=0
  first=0
  is_dedup=0
  prev=""
  note=""
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      "###")
        isrun=0
        first=1
        is_dedup=0
        prev=""
        note=""
        continue
        ;;
      "###end")
        if [ "$isrun" = 1 ] && [ "$is_dedup" = 1 ] && [ -n "$note" ]; then
          printf '%s\n' "$note" >> "$notes"
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
    if [ "$prev" = "-note" ]; then
      note=$line
    fi
    if [ "$line" = "catalog-dedup-batch" ]; then
      is_dedup=1
    fi
    prev=$line
  done < "$argsfile"
  n=$(wc -l < "$notes")
  n=$((n + 0))
  if [ "$n" -ne 3 ]; then
    fail "note count $n want 3 (two propose + execute)"
  fi
  u=$(sort -u "$notes" | wc -l)
  u=$((u + 0))
  if [ "$u" -ne 1 ]; then
    fail "propose/execute -note values differ"
  fi
  val=$(sed -n '1p' "$notes")
  case "$val" in
    *char-xsrc-nightly*) ;;
    *) fail "note missing char-xsrc-nightly: $val" ;;
  esac
}

# --- T1 ---
tstart 1
td=$(mktemp -d)
install_fakes "$td"
run_job "$td"
expect_exit "$td" 0
write_t1_expected "$td/ctl/expected"
expect_seq "$td" "$td/ctl/expected"
expect_reindex "$td" 1
expect_stamp "$td"
expect_env_gone "$td"
expect_no_alert "$td"
tend
rm -rf "$td"

# --- T2 ---
tstart 2
td=$(mktemp -d)
install_fakes "$td"
: > "$td/ctl/empty_worklist"
run_job "$td"
expect_exit "$td" 0
collect_labels "$td"
expect_no_label "$td" propose
expect_no_label "$td" propose-run
expect_label "$td" execute-run
expect_stamp "$td"
tend
rm -rf "$td"

# --- T3 ---
tstart 3
td=$(mktemp -d)
install_fakes "$td"
echo 6 > "$td/ctl/propose_count"
XSRC_PROPOSE_MAX=5 run_job "$td"
expect_exit "$td" 1
collect_labels "$td"
expect_label "$td" propose
expect_no_label "$td" propose-run
expect_label "$td" execute-run
expect_fail_alert "$td"
expect_no_stamp "$td"
tend
rm -rf "$td"

# --- T4 ---
tstart 4
td=$(mktemp -d)
install_fakes "$td"
echo 0 > "$td/ctl/propose_count"
run_job "$td"
expect_exit "$td" 0
collect_labels "$td"
expect_label "$td" propose
expect_no_label "$td" propose-run
expect_stamp "$td"
tend
rm -rf "$td"

# --- T5 ---
tstart 5
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/17 04:00:00 INFO adjudicate done prompt_version=person-adjudicate-v1 packets=12 skipped_already_judged=77 judged=10 merge=0 distinct=0 unsure=0 errors=2 elapsed=3s' \
  > "$td/ctl/out/adjudicate-r1"
echo 1 > "$td/ctl/rc/adjudicate-r1"
run_job "$td"
expect_exit "$td" 0
collect_labels "$td"
expect_label "$td" execute-run
expect_stamp "$td"
expect_no_alert "$td"
tend
rm -rf "$td"

# --- T6 ---
tstart 6
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/17 04:00:00 INFO adjudicate done prompt_version=person-adjudicate-v1 packets=41 skipped_already_judged=99 judged=1 merge=0 distinct=0 unsure=0 errors=40 elapsed=3s' \
  > "$td/ctl/out/adjudicate-r1"
echo 1 > "$td/ctl/rc/adjudicate-r1"
run_job "$td"
expect_exit "$td" 0
collect_labels "$td"
expect_label "$td" execute-run
expect_stamp "$td"
expect_xsrc_alert_only "$td"
tend
rm -rf "$td"

# --- T7 ---
tstart 7
td=$(mktemp -d)
install_fakes "$td"
: > "$td/ctl/out/adjudicate-panel"
echo 1 > "$td/ctl/rc/adjudicate-panel"
run_job "$td"
expect_exit "$td" 1
collect_labels "$td"
expect_no_label "$td" emit-all
expect_no_label "$td" propose
expect_no_label "$td" propose-run
expect_no_label "$td" execute-run
expect_fail_alert "$td"
expect_no_stamp "$td"
expect_env_gone "$td"
tend
rm -rf "$td"

# --- T8: a stale done line from an earlier run the same day must not count ---
tstart 8
td=$(mktemp -d)
install_fakes "$td"
mkdir -p "$td/base/logs"
printf '%s\n' '2026/09/17 04:00:00 INFO adjudicate done prompt_version=person-adjudicate-v1 packets=99 skipped_already_judged=99 judged=1 merge=0 distinct=0 unsure=0 errors=40 elapsed=3s' \
  > "$td/base/logs/run-$(date -u +%F).log"
: > "$td/ctl/out/adjudicate-r1"
echo 1 > "$td/ctl/rc/adjudicate-r1"
run_job "$td"
expect_exit "$td" 1
expect_fail_alert "$td"
expect_no_stamp "$td"
tend
rm -rf "$td"

# --- T9 ---
tstart 9
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' "$td/adj.lock" > "$td/ctl/flock_wait_fail"
run_job "$td"
expect_exit "$td" 1
expect_fail_alert "$td"
expect_no_docker "$td"
expect_no_stamp "$td"
tend
rm -rf "$td"

# --- T10: a run skipped by the lock must not shred the running run's env snapshot ---
tstart 10
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

# --- T11 ---
tstart 11
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' "$td/mt.lock" > "$td/ctl/flock_wait_fail"
run_job "$td"
expect_exit "$td" 1
expect_fail_alert "$td"
expect_no_stamp "$td"
expect_reindex "$td" 0
tend
rm -rf "$td"

# --- T12 ---
tstart 12
td=$(mktemp -d)
install_fakes "$td"
echo 0 > "$td/ctl/execute_count"
run_job "$td"
expect_exit "$td" 0
expect_reindex "$td" 0
tend
rm -rf "$td"

# --- T13 ---
tstart 13
td=$(mktemp -d)
install_fakes "$td"
: > "$td/ctl/propose_silent"
run_job "$td"
expect_exit "$td" 1
collect_labels "$td"
expect_label "$td" propose
expect_no_label "$td" propose-run
tend
rm -rf "$td"

# --- T14 ---
tstart 14
td=$(mktemp -d)
install_fakes "$td"
run_job "$td"
expect_exit "$td" 0
scan_t14 "$td"
tend
rm -rf "$td"

# --- T15 ---
tstart 15
td=$(mktemp -d)
install_fakes "$td"
run_job "$td"
expect_exit "$td" 0
scan_t15 "$td"
tend
rm -rf "$td"

# --- T16 ---
tstart 16
td=$(mktemp -d)
install_fakes "$td"
run_job "$td"
expect_exit "$td" 0
scan_t16 "$td"
tend
rm -rf "$td"

if [ "$FAILED" -ne 0 ]; then
  exit 1
fi
exit 0
