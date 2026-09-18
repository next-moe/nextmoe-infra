#!/bin/sh
# Offline harness for run.sh beside it: fake docker / flock / shred / alert on
# PATH, so it never reaches Docker, the network or a database. Run it on a dev
# box before redeploying run.sh — this directory has no CI.
#
#   sh scripts/prod-cron/source-import/test.sh
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

write_t1_expected() {
  cat > "$1" <<'EOF'
import-getchu-refs --apply
import-getchu-intros --population all --apply
import-getchu-characters --apply
import-store-anchors --only dmm --apply
import-store-anchors --only dlsite --apply
import-store-anchors --only dlsite-en --apply
import-release-labels --apply
backfill-character-instances --apply
import-work-intro --apply
reconcile-eg-anchors --apply
reconcile-eg-works --apply --limit 250
import-character-roster --source eg --apply
import-galgame-credits --source eg --apply
import-galgame-credits --source eg-music --apply
import-store-refs --apply
backfill-work-playtime --source eg --apply
expand-bgm-type4-gated --apply --limit 250
import-character-roster --source bangumi --apply
backfill-bgm-zh-names --lane character --apply
backfill-bgm-zh-names --lane person --apply
backfill-bgm-zh-names --lane label --apply
import-entity-aliases --hints --run
import-entity-aliases --candidates --run
person-link-batch --rule-set alias --actor 1 --run
import-bangumi-xmedia --run
import-eg-dlsite-releases --run
backfill-dlsite-genres --apply
backfill-dlsite-media --kind intro --apply
import-work-aliases --source all --apply
import-work-platforms --source all --apply
import-work-series --apply
reconcile-org-labels --source all --apply
EOF
}

install_fakes() {
  td=$1
  mkdir -p "$td/bin" "$td/sentinel" "$td/base" "$td/vndb" "$td/lib" "$td/ctl/out" "$td/ctl/rc"
  : > "$td/vndb/.lock"

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
    import-getchu-characters \
    import-getchu-intros \
    import-getchu-refs \
    import-store-anchors \
    import-release-labels \
    backfill-character-instances \
    import-work-intro \
    import-character-roster \
    import-galgame-credits \
    import-store-refs \
    backfill-work-playtime \
    backfill-bgm-zh-names \
    import-entity-aliases \
    person-link-batch \
    import-bangumi-xmedia \
    expand-bgm-type4-gated \
    import-eg-dlsite-releases \
    backfill-dlsite-genres \
    backfill-dlsite-media \
    import-work-aliases \
    import-work-platforms \
    import-work-series \
    reconcile-eg-anchors \
    reconcile-eg-works \
    reconcile-org-labels
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

emit_out() {
  key=$1
  if [ -f "$CTL/out/$key" ]; then
    cat "$CTL/out/$key"
    return
  fi
  case "$key" in
    import-character-roster+eg+dry|import-character-roster+eg+apply)
      echo '2026/09/16 13:28:03 INFO roster import summary source=eg characters_created=24 attached_existing=0 aliases_created=0 edges_written=27863 already=0 skipped_no_work_anchor=2776 skipped_no_name=0 skipped_claimed_probable=0 skipped_retired_exact_squat=0 portrait_candidates=0 errors=0'
      ;;
    import-galgame-credits+eg+dry|import-galgame-credits+eg+apply)
      echo '2026/09/16 13:28:04 INFO import summary source=eg names_created=94 labels_created=0 characters_created=24 credits_written=189879 skipped_unmapped_role=0 skipped_gate=0 skipped_claimed_probable_name=0 skipped_claimed_probable_char=0 skipped_retired_exact_char=0 already=0 errors=0'
      ;;
    import-galgame-credits+eg-music+dry|import-galgame-credits+eg-music+apply)
      echo '2026/09/16 13:28:04 INFO import summary source=eg-music names_created=18 labels_created=0 characters_created=0 credits_written=24755 skipped_unmapped_role=0 skipped_gate=0 skipped_claimed_probable_name=0 skipped_claimed_probable_char=0 skipped_retired_exact_char=0 already=0 errors=0'
      ;;
    import-character-roster+bangumi+dry|import-character-roster+bangumi+apply)
      echo '2026/09/16 13:28:27 INFO roster import summary source=bangumi characters_created=0 attached_existing=0 aliases_created=0 edges_written=101357 already=0 skipped_no_work_anchor=0 skipped_no_name=1 skipped_claimed_probable=0 skipped_retired_exact_squat=0 portrait_candidates=0 errors=0'
      ;;
    import-entity-aliases+hints+apply)
      echo 'leg A APPLIED — hints written: bgm_names=0 bgm_labels=0 bgm_chars=0 eg_names=0 | skipped_same=24171 skipped_role=283 already=111488'
      ;;
    import-entity-aliases+candidates+dry)
      echo 'leg B DRY-RUN — alias_declared candidates=12 (bidirectional=0) | ambiguous=40 already_candidate=2174 already_same_person=389'
      ;;
    person-link-batch+dry)
      echo '[create] A3   "作家A" ↔ "作家A(ペンネーム)"'
      echo 'DRY-RUN (nothing written; pass --run to link) [alias] — A1=0 A2=0 A3=8 A4=0 | created=3 attached=5 needs_manual=0 already=0 errors=0 | unmatched=4 of 12'
      ;;
    import-bangumi-xmedia+dry|import-bangumi-xmedia+apply)
      echo '2026/09/16 16:30:46 INFO bangumi cross-media wave summary registered_anime=12 registered_manga=3 registered_novel=1 edges=20 edges_written=0 already_edge=2011 already_work=1958 skipped_platform=143 skipped_no_title=0 skipped_self=2 errors=0'
      ;;
    expand-bgm-type4-gated+dry)
      echo '2026/09/17 05:21:17 INFO bgm-type4-gated survey dry=true pool_total=81364 excluded_console_mobile=13707 eligible_pool=67657 sig_p=229 sig_t=695 sig_x=1552 gated_total=1767 skipped_ascii_xonly=1005 title_collisions=1369 skipped_intra_collision=24 to_create=12 to_quarantine=1369 quarantined=0 works_created=0 titles_created=0 anchors_created=0 revisions_created=0'
      echo '2026/09/17 05:21:17 INFO bgm-type4-gated summary pool_total=81364 excluded_console_mobile=13707 eligible_pool=67657 sig_p=229 sig_t=695 sig_x=1552 gated_total=1767 skipped_ascii_xonly=1005 title_collisions=1369 skipped_intra_collision=24 to_create=12 to_quarantine=1369 quarantined=0 works_created=0 titles_created=0 anchors_created=0 revisions_created=0'
      ;;
    import-eg-dlsite-releases+dry)
      echo '2026/09/17 05:21:23 INFO eg-dlsite wave summary attached=0 minted=46 already=14178 ambiguous=407 missing=782 amb_b1_attach=0 amb_b2_mint=0 amb_b3_conflict=0 releases=46 titles=46 labels=31 names=49 credits=196 edges=46 eg_refs=46 stubs=0 skipped_unmapped_role=0 title_collisions=23 quarantined=23 skipped_intra_collision=0 errors=0'
      ;;
    import-work-series+dry)
      echo '2026/09/16 13:28:23 INFO workseries done apply=false anchored_works=19709 series_eligible=887 members_wanted=3178 series_created=10 series_renamed=0 series_deleted=0 members_added=1100 members_stale=0 order_changed=288 errors=0'
      ;;
    import-work-series+apply)
      echo '2026/09/16 13:28:23 INFO workseries done apply=true anchored_works=19709 series_eligible=887 members_wanted=3178 series_created=10 series_renamed=0 series_deleted=0 members_added=1100 members_stale=0 order_changed=288 errors=0'
      ;;
    reconcile-eg-anchors+dry|reconcile-eg-anchors+apply)
      echo '2026/09/18 02:00:00 INFO eganchors summary games=0 anchored_games=0 no_evidence_games=0 multi_games=0 twin_games=0 candidate_games=0 rejected_skips=0 exact_planned=0 probable_planned=0 related_planned=0 corroborated=0 written=0 exists=0 errors=0'
      ;;
    reconcile-eg-works+dry|reconcile-eg-works+apply)
      echo '2026/09/18 04:00:00 INFO egworks summary population=0 pack_games=0 port_games=0 attached=0 quarantined=0 minted_live=0 edition_folded=0 rejected_skips=0 limited=0 refs_planned=0 candidates_planned=0 written=0 errors=0'
      ;;
    reconcile-org-labels+all+dry|reconcile-org-labels+all+apply)
      echo '2026/09/17 14:21:47 INFO org-label anchor source done source=vndb pass=1 apply=false orgs=30089 already=25082 exact=8 probable=16 new_labels=41 new_edges=52 conflict=1421 skip_no_match=922 skip_ambiguous=21 skip_ungradeable=2437 skip_rejected=0 skip_deferred=0 vndb_in_anchored=7'
      echo '2026/09/17 14:21:47 INFO org-label anchor source done source=eg pass=1 apply=false orgs=7565 already=5094 exact=9 probable=71 new_labels=3 new_edges=3 conflict=110 skip_no_match=1770 skip_ambiguous=106 skip_ungradeable=404 skip_rejected=1 skip_deferred=0 vndb_in_anchored=0'
      echo '2026/09/17 14:21:48 INFO org-label spine pass done pass=1 apply=false considered=108 minted=4 anchored=2 candidates=22 skip_claimed=64 skip_edgeless=66 skip_alias_only=2 skip_loose_twin=3 skip_deferred=2 errors=0'
      echo '2026/09/17 14:21:48 INFO reconcile-org-labels summary source=all apply=false orgs=48155 already=33369 anchors_exact=17 anchors_probable=89 new_labels=44 new_edges=55 conflict=1566 skip_no_match=9921 skip_ambiguous=169 skip_ungradeable=2841 skip_rejected=1 skip_deferred=0 vndb_in_anchored=7 errors=0'
      echo '2026/09/17 14:21:48 INFO reconcile-org-labels spine summary considered=108 minted=4 anchored=2 candidates=22 skip_claimed=64 skip_edgeless=66 skip_alias_only=2 skip_loose_twin=3 skip_deferred=2 errors=0'
      ;;
    *)
      echo "fake-ok $key"
      ;;
  esac
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
      import-getchu-characters \
      import-getchu-intros \
      import-getchu-refs \
      import-store-anchors \
      import-release-labels \
      backfill-character-instances \
      import-work-intro \
      import-character-roster \
      import-galgame-credits \
      import-store-refs \
      backfill-work-playtime \
      backfill-bgm-zh-names \
      import-entity-aliases \
      person-link-batch \
      import-bangumi-xmedia \
      expand-bgm-type4-gated \
      import-eg-dlsite-releases \
      backfill-dlsite-genres \
      backfill-dlsite-media \
      import-work-aliases \
      import-work-platforms \
      import-work-series \
      reconcile-eg-anchors \
      reconcile-eg-works \
      reconcile-org-labels
    do
      case "$toolcmd" in
        *"$t"*) tool=$t; break ;;
      esac
    done
    if [ -z "$tool" ]; then
      echo "fake docker run: cannot identify tool: $toolcmd" >&2
      exit 99
    fi

    src=""; only=""; lane=""; pop=""; hints=""; limit=""; leg=""; ruleset=""; actor=""; kind=""
    case "$toolcmd" in
      *"--source eg-music"*) src=eg-music ;;
      *"--source eg"*) src=eg ;;
      *"--source bangumi"*) src=bangumi ;;
      *"--source all"*) src=all ;;
    esac
    case "$toolcmd" in
      *"--only dlsite-en"*) only=dlsite-en ;;
      *"--only dlsite"*) only=dlsite ;;
      *"--only dmm"*) only=dmm ;;
    esac
    case "$toolcmd" in
      *"--lane character"*) lane=character ;;
      *"--lane person"*) lane=person ;;
      *"--lane label"*) lane=label ;;
    esac
    case "$toolcmd" in
      *"--population all"*) pop=all ;;
    esac
    case "$toolcmd" in
      *"--hints"*) hints=1; leg=hints ;;
      *"--candidates"*) leg=candidates ;;
    esac
    case "$toolcmd" in
      *"--rule-set alias"*) ruleset=alias ;;
      *"--rule-set shared"*) ruleset=shared ;;
    esac
    case "$toolcmd" in
      *"--actor 1"*) actor=1 ;;
    esac
    case "$toolcmd" in
      *"--limit "*) limit=$(printf '%s\n' "$toolcmd" | sed 's/.*--limit \([0-9]*\).*/\1/') ;;
    esac
    case "$toolcmd" in
      *"--kind "*) kind=$(printf '%s\n' "$toolcmd" | sed 's/.*--kind \([a-z,]*\).*/\1/') ;;
    esac
    if [ "$tool" = "import-entity-aliases" ] && [ -z "$leg" ]; then
      echo "import-entity-aliases without --hints or --candidates runs both legs" >> "$CTL/violations"
    fi
    if [ "$tool" = "person-link-batch" ] && { [ "$ruleset" != alias ] || [ "$actor" != 1 ]; }; then
      echo "person-link-batch must run --rule-set alias --actor 1: $toolcmd" >> "$CTL/violations"
    fi
    if [ "$tool" = "backfill-dlsite-media" ] && [ "$kind" != intro ]; then
      echo "backfill-dlsite-media must run --kind intro (covers and screenshots are image-mirror's): $toolcmd" >> "$CTL/violations"
    fi
    case "$tool" in
      import-entity-aliases|import-bangumi-xmedia|import-eg-dlsite-releases|person-link-batch)
        case "$toolcmd" in *"--apply"*) echo "$tool takes --run, not --apply" >> "$CTL/violations" ;; esac
        ;;
      *)
        case "$toolcmd" in *"--run"*) echo "$tool takes --apply, not --run" >> "$CTL/violations" ;; esac
        ;;
    esac
    mode=dry
    case "$toolcmd" in
      *"--apply"*) mode=apply ;;
      *"--run"*) mode=apply ;;
    esac

    key=$tool
    [ -n "$leg" ] && key="$key+$leg"
    [ -n "$src" ] && key="$key+$src"
    [ -n "$only" ] && key="$key+$only"
    [ -n "$lane" ] && key="$key+$lane"
    key="$key+$mode"
    printf '%s\n' "$key" >> "$CTL/tools.log"

    if [ "$mode" = apply ]; then
      line=$tool
      [ -n "$src" ] && line="$line --source $src"
      [ -n "$only" ] && line="$line --only $only"
      [ -n "$lane" ] && line="$line --lane $lane"
      [ -n "$pop" ] && line="$line --population $pop"
      [ -n "$hints" ] && line="$line --hints"
      [ "$leg" = candidates ] && line="$line --candidates"
      [ -n "$ruleset" ] && line="$line --rule-set $ruleset"
      [ -n "$actor" ] && line="$line --actor $actor"
      [ -n "$kind" ] && line="$line --kind $kind"
      case "$tool" in
        import-entity-aliases|import-bangumi-xmedia|import-eg-dlsite-releases|person-link-batch) line="$line --run" ;;
        *) line="$line --apply" ;;
      esac
      [ -n "$limit" ] && line="$line --limit $limit"
      printf '%s\n' "$line" >> "$CTL/apply.log"
    fi

    emit_out "$key"
    rc=0
    if [ -f "$CTL/rc/$key" ]; then
      rc=$(cat "$CTL/rc/$key")
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

if [ "$1" = "-w" ]; then
  if [ "${FLOCK_WAIT_FAIL:-0}" = 1 ]; then
    exit 1
  fi
  if [ $# -lt 3 ]; then
    echo "fake flock -w: need timeout and path" >&2
    exit 99
  fi
  shift 2
  shift
  if [ $# -eq 0 ]; then
    exit 0
  fi
  exec "$@"
fi

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
  export SOURCE_IMPORT_BASE="$td/base"
  export SOURCE_IMPORT_VNDB_LOCK="$td/vndb/.lock"
  export SOURCE_IMPORT_ALERT="$td/lib/alert.sh"
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

apply_log() {
  if [ -f "$1/ctl/apply.log" ]; then
    cat "$1/ctl/apply.log"
  fi
}

expect_apply() {
  td=$1
  exp=$2
  got="$td/ctl/apply.got"
  apply_log "$td" > "$got"
  if ! diff -u "$exp" "$got"; then
    fail "apply order mismatch"
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

scan_t9() {
  argsfile=$1
  if [ ! -f "$argsfile" ]; then
    fail "no recorded docker argv"
    return 0
  fi
  skip=0
  nrun=0
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
      "###end") continue ;;
      "###"*) skip=0; continue ;;
    esac
    if [ "$skip" = 1 ]; then
      skip=0
      continue
    fi
    if [ "$line" = "run" ]; then
      nrun=$((nrun + 1))
    fi
    if [ "$line" = "-c" ]; then
      skip=1
      continue
    fi
    case "$line" in
      *password=*|*dbname=*)
        fail "argv leak: $line"
        ;;
    esac
  done < "$argsfile"
  if [ "$nrun" -eq 0 ]; then
    fail "no docker run invocations recorded"
  fi
}

# --- T1 ---
tstart 1
td=$(mktemp -d)
install_fakes "$td"
run_job "$td"
expect_exit "$td" 0
if ! has_stamp "$td"; then fail "missing stamp"; fi
if has_alert "$td"; then fail "unexpected alert"; fi
write_t1_expected "$td/ctl/expected"
expect_apply "$td" "$td/ctl/expected"
tend
rm -rf "$td"

# --- T2 ---
tstart 2
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/16 13:28:03 INFO roster import summary source=eg characters_created=301 attached_existing=0 aliases_created=0 edges_written=0 already=0 errors=0' \
  > "$td/ctl/out/import-character-roster+eg+dry"
run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! has_alert "$td"; then fail "no alert"; fi
write_t1_expected "$td/ctl/t1"
grep -v -F \
  -e 'import-character-roster --source eg --apply' \
  -e 'import-galgame-credits --source eg --apply' \
  -e 'import-galgame-credits --source eg-music --apply' \
  -e 'import-store-refs --apply' \
  -e 'backfill-work-playtime --source eg --apply' \
  "$td/ctl/t1" > "$td/ctl/expected"
expect_apply "$td" "$td/ctl/expected"
tend
rm -rf "$td"

# --- T3 ---
tstart 3
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/16 13:28:03 INFO roster import summary source=eg attached_existing=0 aliases_created=0 edges_written=0 already=0 errors=0' \
  > "$td/ctl/out/import-character-roster+eg+dry"
run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! has_alert "$td"; then fail "no alert"; fi
write_t1_expected "$td/ctl/t1"
grep -v -F \
  -e 'import-character-roster --source eg --apply' \
  -e 'import-galgame-credits --source eg --apply' \
  -e 'import-galgame-credits --source eg-music --apply' \
  -e 'import-store-refs --apply' \
  -e 'backfill-work-playtime --source eg --apply' \
  "$td/ctl/t1" > "$td/ctl/expected"
expect_apply "$td" "$td/ctl/expected"
tend
rm -rf "$td"

# --- T4 ---
tstart 4
td=$(mktemp -d)
install_fakes "$td"
echo 1 > "$td/ctl/rc/import-store-anchors+dmm+apply"
run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! has_alert "$td"; then fail "no alert"; fi
write_t1_expected "$td/ctl/t1"
grep -v -F \
  -e 'import-store-anchors --only dlsite --apply' \
  -e 'import-store-anchors --only dlsite-en --apply' \
  -e 'import-release-labels --apply' \
  -e 'backfill-character-instances --apply' \
  -e 'import-work-intro --apply' \
  "$td/ctl/t1" > "$td/ctl/expected"
expect_apply "$td" "$td/ctl/expected"
tend
rm -rf "$td"

# --- T5 ---
tstart 5
td=$(mktemp -d)
install_fakes "$td"
FLOCK_WAIT_FAIL=1 run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! has_alert "$td"; then fail "no alert"; fi
if [ -f "$td/ctl/docker.args" ]; then fail "docker ran"; fi
if [ -f "$td/ctl/apply.log" ]; then fail "tools ran"; fi
tend
rm -rf "$td"

# --- T6 ---
tstart 6
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/16 13:28:23 INFO workseries done apply=false anchored_works=19709 series_eligible=887 members_wanted=3178 series_created=10 series_renamed=0 series_deleted=11 members_added=1100 members_stale=0 order_changed=288 errors=0' \
  > "$td/ctl/out/import-work-series+dry"
run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! has_alert "$td"; then fail "no alert"; fi
write_t1_expected "$td/ctl/t1"
grep -v -F -e 'import-work-series --apply' "$td/ctl/t1" > "$td/ctl/expected"
expect_apply "$td" "$td/ctl/expected"
tend
rm -rf "$td"

# --- T7: a cross-media backlog past its ceiling registers nothing ---
tstart 7
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/16 16:30:46 INFO bangumi cross-media wave summary registered_anime=450 registered_manga=247 registered_novel=278 edges=1157 edges_written=0 already_edge=2011 already_work=1958 skipped_platform=143 skipped_no_title=0 skipped_self=0 errors=0' \
  > "$td/ctl/out/import-bangumi-xmedia+dry"
run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! has_alert "$td"; then fail "no alert"; fi
write_t1_expected "$td/ctl/t1"
grep -v -F -e 'import-bangumi-xmedia --run' "$td/ctl/t1" > "$td/ctl/expected"
expect_apply "$td" "$td/ctl/expected"
tend
rm -rf "$td"

# --- T8 ---
tstart 8
td=$(mktemp -d)
install_fakes "$td"
FLOCK_HOLD_FAIL=1 run_job "$td"
expect_exit "$td" 0
if has_stamp "$td"; then fail "stamp written"; fi
if has_alert "$td"; then fail "unexpected alert"; fi
if [ -f "$td/ctl/docker.args" ]; then fail "docker ran"; fi
if [ -f "$td/ctl/apply.log" ]; then fail "tools ran"; fi
tend
rm -rf "$td"

# --- T9 ---
tstart 9
td=$(mktemp -d)
install_fakes "$td"
run_job "$td"
expect_exit "$td" 0
scan_t9 "$td/ctl/docker.args"
tend
rm -rf "$td"

# --- T10: a run skipped by the lock must not shred the running run's env snapshot ---
tstart 10
td=$(mktemp -d)
install_fakes "$td"
mkdir -p "$td/base"
echo 'KUN_PG_PASSWORD=held-by-the-running-instance' > "$td/base/env.tmp"
FLOCK_HOLD_FAIL=1 run_job "$td"
expect_exit "$td" 0
if [ ! -f "$td/base/env.tmp" ]; then fail "lock-skipped run deleted the running run's env.tmp"; fi
tend
rm -rf "$td"

# --- T11: a vndb-lock timeout must not shred it either ---
tstart 11
td=$(mktemp -d)
install_fakes "$td"
mkdir -p "$td/base"
echo 'KUN_PG_PASSWORD=held-by-the-running-instance' > "$td/base/env.tmp"
FLOCK_WAIT_FAIL=1 run_job "$td"
expect_exit_nonzero "$td"
if [ ! -f "$td/base/env.tmp" ]; then fail "timed-out run deleted the running run's env.tmp"; fi
tend
rm -rf "$td"

# --- T12: import-entity-aliases never runs its review-queue leg, and every tool gets its own apply flag ---
tstart 12
td=$(mktemp -d)
install_fakes "$td"
run_job "$td"
expect_exit "$td" 0
if [ -s "$td/ctl/violations" ]; then fail "$(cat "$td/ctl/violations")"; fi
if ! grep -q '^import-entity-aliases+hints+apply$' "$td/ctl/tools.log"; then fail "entity-aliases hints never ran"; fi
if ! grep -q '^person-link-batch+apply$' "$td/ctl/tools.log"; then fail "person-link-batch never ran"; fi
tend
rm -rf "$td"

# --- T13: an EG-DLsite mint batch past its ceiling mints nothing, and the rest of the DLsite group stands down ---
tstart 13
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/17 05:21:23 INFO eg-dlsite wave summary attached=0 minted=151 already=14178 ambiguous=407 missing=782 title_collisions=0 quarantined=0 skipped_intra_collision=0 errors=0' \
  > "$td/ctl/out/import-eg-dlsite-releases+dry"
run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! has_alert "$td"; then fail "no alert"; fi
write_t1_expected "$td/ctl/t1"
grep -v -F \
  -e 'import-eg-dlsite-releases --run' \
  -e 'backfill-dlsite-genres --apply' \
  -e 'backfill-dlsite-media --kind intro --apply' \
  -e 'import-work-aliases --source all --apply' \
  -e 'import-work-platforms --source all --apply' \
  -e 'import-work-series --apply' \
  "$td/ctl/t1" > "$td/ctl/expected"
expect_apply "$td" "$td/ctl/expected"
tend
rm -rf "$td"

# --- T14: a quarantine batch past its ceiling does the same ---
tstart 14
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/17 05:21:23 INFO eg-dlsite wave summary attached=0 minted=60 already=14178 title_collisions=51 quarantined=51 skipped_intra_collision=0 errors=0' \
  > "$td/ctl/out/import-eg-dlsite-releases+dry"
run_job "$td"
expect_exit_nonzero "$td"
if ! has_alert "$td"; then fail "no alert"; fi
if grep -q -F 'import-eg-dlsite-releases --run' "$td/ctl/apply.log"; then fail "eg-dlsite applied past its quarantine ceiling"; fi
tend
rm -rf "$td"

# --- T15: a Bangumi live-create batch past its ceiling mints nothing, and the rest of the group stands down ---
tstart 15
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/17 05:21:17 INFO bgm-type4-gated summary pool_total=81364 gated_total=1767 title_collisions=1369 to_create=151 to_quarantine=1369 works_created=0' \
  > "$td/ctl/out/expand-bgm-type4-gated+dry"
run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! has_alert "$td"; then fail "no alert"; fi
write_t1_expected "$td/ctl/t1"
grep -v -F \
  -e 'expand-bgm-type4-gated --apply --limit 250' \
  -e 'import-character-roster --source bangumi --apply' \
  -e 'backfill-bgm-zh-names --lane character --apply' \
  -e 'backfill-bgm-zh-names --lane person --apply' \
  -e 'backfill-bgm-zh-names --lane label --apply' \
  -e 'import-entity-aliases --hints --run' \
  -e 'import-entity-aliases --candidates --run' \
  -e 'person-link-batch --rule-set alias --actor 1 --run' \
  -e 'import-bangumi-xmedia --run' \
  "$td/ctl/t1" > "$td/ctl/expected"
expect_apply "$td" "$td/ctl/expected"
tend
rm -rf "$td"

# --- T16: an unreadable Bangumi survey is a failure, not a pass ---
tstart 16
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/17 05:21:17 INFO bgm-type4-gated summary pool_total=81364 gated_total=1767' \
  > "$td/ctl/out/expand-bgm-type4-gated+dry"
run_job "$td"
expect_exit_nonzero "$td"
if ! has_alert "$td"; then fail "no alert"; fi
if grep -q -F 'expand-bgm-type4-gated' "$td/ctl/apply.log"; then fail "bgm-type4 applied without a readable survey"; fi
tend
rm -rf "$td"

# --- T17: a burst of alias-declared pairs files nothing and links nothing ---
tstart 17
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' 'leg B DRY-RUN — alias_declared candidates=301 (bidirectional=3) | ambiguous=40 already_candidate=2174 already_same_person=389' \
  > "$td/ctl/out/import-entity-aliases+candidates+dry"
run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! has_alert "$td"; then fail "no alert"; fi
write_t1_expected "$td/ctl/t1"
grep -v -F \
  -e 'import-entity-aliases --candidates --run' \
  -e 'person-link-batch --rule-set alias --actor 1 --run' \
  -e 'import-bangumi-xmedia --run' \
  "$td/ctl/t1" > "$td/ctl/expected"
expect_apply "$td" "$td/ctl/expected"
tend
rm -rf "$td"

# --- T18: a link batch past its ceiling creates no one ---
tstart 18
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' 'DRY-RUN (nothing written; pass --run to link) [alias] — A1=0 A2=0 A3=250 A4=0 | created=201 attached=49 needs_manual=0 already=0 errors=0 | unmatched=0 of 250' \
  > "$td/ctl/out/person-link-batch+dry"
run_job "$td"
expect_exit_nonzero "$td"
if ! has_alert "$td"; then fail "no alert"; fi
write_t1_expected "$td/ctl/t1"
grep -v -F \
  -e 'person-link-batch --rule-set alias --actor 1 --run' \
  -e 'import-bangumi-xmedia --run' \
  "$td/ctl/t1" > "$td/ctl/expected"
expect_apply "$td" "$td/ctl/expected"
tend
rm -rf "$td"

# --- T19: so does an attach burst, and an unreadable link plan ---
tstart 19
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' 'DRY-RUN (nothing written; pass --run to link) [alias] — A1=0 A2=0 A3=301 A4=0 | created=0 attached=301 needs_manual=0 already=0 errors=0 | unmatched=0 of 301' \
  > "$td/ctl/out/person-link-batch+dry"
run_job "$td"
expect_exit_nonzero "$td"
if grep -q -F 'person-link-batch' "$td/ctl/apply.log"; then fail "linked past the attach ceiling"; fi
rm -rf "$td"
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' 'person-link-batch: panic before the summary' > "$td/ctl/out/person-link-batch+dry"
run_job "$td"
expect_exit_nonzero "$td"
if grep -q -F 'person-link-batch' "$td/ctl/apply.log"; then fail "linked without a readable plan"; fi
tend
rm -rf "$td"

# --- T20: a label batch past its ceiling mints nothing; the ceiling reads the run total, not the first source ---
tstart 20
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' \
  'INFO org-label anchor source done source=vndb pass=1 apply=false new_labels=60 new_edges=70' \
  'INFO org-label anchor source done source=eg pass=1 apply=false new_labels=41 new_edges=41' \
  'INFO org-label spine pass done pass=1 apply=false considered=10 minted=1 anchored=0' \
  'INFO reconcile-org-labels summary source=all apply=false anchors_exact=1 anchors_probable=2 new_labels=101 new_edges=111 errors=0' \
  'INFO reconcile-org-labels spine summary considered=10 minted=1 anchored=0 errors=0' \
  > "$td/ctl/out/reconcile-org-labels+all+dry"
run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! has_alert "$td"; then fail "no alert"; fi
write_t1_expected "$td/ctl/t1"
grep -v -F -e 'reconcile-org-labels --source all --apply' "$td/ctl/t1" > "$td/ctl/expected"
expect_apply "$td" "$td/ctl/expected"
tend
rm -rf "$td"

# --- T21: so does a corporate-graph burst, and a probable-anchor burst ---
tstart 21
for burst in 'minted=31 anchored=0' 'anchors_probable=301'; do
  td=$(mktemp -d)
  install_fakes "$td"
  case "$burst" in
    minted=*) spine=$burst; probable=2 ;;
    *) spine='minted=1 anchored=0'; probable=301 ;;
  esac
  printf '%s\n' \
    "INFO reconcile-org-labels summary source=all apply=false anchors_exact=1 anchors_probable=$probable new_labels=5 new_edges=5 errors=0" \
    "INFO reconcile-org-labels spine summary considered=40 $spine errors=0" \
    > "$td/ctl/out/reconcile-org-labels+all+dry"
  run_job "$td"
  expect_exit_nonzero "$td"
  if grep -q -F 'reconcile-org-labels' "$td/ctl/apply.log"; then fail "applied past the ceiling ($burst)"; fi
  rm -rf "$td"
done
tend

# --- T22: an unreadable label plan is a failure, not a pass ---
tstart 22
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' 'reconcile-org-labels failed error="load vndb orgs: connection refused"' > "$td/ctl/out/reconcile-org-labels+all+dry"
run_job "$td"
expect_exit_nonzero "$td"
if ! has_alert "$td"; then fail "no alert"; fi
if grep -q -F 'reconcile-org-labels' "$td/ctl/apply.log"; then fail "applied without a readable plan"; fi
tend
rm -rf "$td"

# --- T23: an EG-anchor batch past its ceiling writes nothing and the rest of the EG group stands down ---
tstart 23
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/18 02:00:00 INFO eganchors summary games=9000 anchored_games=0 no_evidence_games=0 multi_games=0 twin_games=0 candidate_games=9000 rejected_skips=0 exact_planned=301 probable_planned=0 related_planned=0 corroborated=0 written=0 exists=0 errors=0' \
  > "$td/ctl/out/reconcile-eg-anchors+dry"
run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! has_alert "$td"; then fail "no alert"; fi
write_t1_expected "$td/ctl/t1"
grep -v -F \
  -e 'reconcile-eg-anchors --apply' \
  -e 'reconcile-eg-works --apply --limit 250' \
  -e 'import-character-roster --source eg --apply' \
  -e 'import-galgame-credits --source eg --apply' \
  -e 'import-galgame-credits --source eg-music --apply' \
  -e 'import-store-refs --apply' \
  -e 'backfill-work-playtime --source eg --apply' \
  "$td/ctl/t1" > "$td/ctl/expected"
expect_apply "$td" "$td/ctl/expected"
tend
rm -rf "$td"

# --- T24: an EG-works batch past its ceiling writes nothing and the rest of the EG group stands down ---
tstart 24
td=$(mktemp -d)
install_fakes "$td"
printf '%s\n' '2026/09/18 04:00:00 INFO egworks summary population=9000 pack_games=0 port_games=0 attached=301 quarantined=0 minted_live=0 edition_folded=0 rejected_skips=0 limited=0 refs_planned=301 candidates_planned=0 written=0 errors=0' \
  > "$td/ctl/out/reconcile-eg-works+dry"
run_job "$td"
expect_exit_nonzero "$td"
if has_stamp "$td"; then fail "stamp written"; fi
if ! has_alert "$td"; then fail "no alert"; fi
write_t1_expected "$td/ctl/t1"
grep -v -F \
  -e 'reconcile-eg-works --apply --limit 250' \
  -e 'import-character-roster --source eg --apply' \
  -e 'import-galgame-credits --source eg --apply' \
  -e 'import-galgame-credits --source eg-music --apply' \
  -e 'import-store-refs --apply' \
  -e 'backfill-work-playtime --source eg --apply' \
  "$td/ctl/t1" > "$td/ctl/expected"
expect_apply "$td" "$td/ctl/expected"
tend
rm -rf "$td"

if [ "$FAILED" -ne 0 ]; then
  exit 1
fi
exit 0
