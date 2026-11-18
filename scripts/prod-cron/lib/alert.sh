#!/bin/sh
# Shared alert sender for the root-owned maintenance crons.
#
#   /root/lib/alert.sh "<subject>" <body-file|->
#   ALERT_DIGEST=<hash of the findings>   optional: a findings alert is resent
#                                         as soon as its findings change
#
# Sends with infra-tools' ops-alert, which reads the SMTP settings the way the
# services do (environment, then the config store): the password moved into
# the store on 2026-11-16 and the old sender, which read only the oauth
# container's env, failed every alert from then on.
#
# A repeat is suppressed for RENOTIFY hours: pg-backup mailed the same failure
# daily for three weeks and cover-shelf the same 41 findings daily, and both
# were tuned out. A [FAIL] alert is new again once its job has succeeded since
# the last mail. An alert that cannot be sent is queued in
# $STATE/undelivered, which the watchdog reports through the deadman ping.
#
# Exits non-zero if the mail could not be sent. Callers should tolerate that
# (an unsendable alert must not turn a warning into a failed job).
set -eu

SUBJ=${1:?subject required}
BODY=${2:--}
TO=${ALERT_TO:-yuyuyukunkunkun@gmail.com}
OAUTH=${ALERT_OAUTH_CONTAINER:-kun-visual-novel-infra-vqvqbc-oauth-1}
PG=${ALERT_PG_CONTAINER:-kun-visual-novel-infra-vqvqbc-postgres-1}
STATE=${ALERT_STATE_DIR:-/root/lib/state}
JOBS_ROOT=${ALERT_JOBS_ROOT:-/root}
RENOTIFY=${ALERT_RENOTIFY_HOURS:-72}
DIGEST=${ALERT_DIGEST:-}
mkdir -p "$STATE"

KEY=$(printf '%s' "$SUBJ" | sha256sum | cut -c1-16)
MARK="$STATE/alert-$KEY"

job_stamp() {
  case "$BODY" in
    "$JOBS_ROOT"/*/logs/*)
      _job=${BODY#"$JOBS_ROOT"/}
      echo "$JOBS_ROOT/${_job%%/*}/state/last-success"
      ;;
  esac
}

NOTE=""
if [ -f "$MARK" ]; then
  LAST_DIGEST=$(sed -n 1p "$MARK")
  REPEATS=$(sed -n 2p "$MARK")
  REPEATS=${REPEATS:-0}
  AGE=$(( ($(date +%s) - $(stat -c %Y "$MARK")) / 3600 ))
  RECOVERED=0
  case "$SUBJ" in
    "[FAIL]"*)
      STAMP=$(job_stamp)
      if [ -n "$STAMP" ] && [ -f "$STAMP" ] && [ "$STAMP" -nt "$MARK" ]; then RECOVERED=1; fi
      ;;
  esac
  if [ "$RECOVERED" = 0 ] && [ "$DIGEST" = "$LAST_DIGEST" ] && [ "$AGE" -lt "$RENOTIFY" ]; then
    TMP=$(mktemp "$STATE/.alert.XXXXXX")
    printf '%s\n%s\n' "$LAST_DIGEST" "$((REPEATS + 1))" > "$TMP"
    touch -r "$MARK" "$TMP"
    mv "$TMP" "$MARK"
    echo "alert: same alert sent ${AGE}h ago; repeat $((REPEATS + 1)) suppressed"
    exit 0
  fi
  if [ "$REPEATS" -gt 0 ]; then
    NOTE="$REPEATS identical alert(s) suppressed since $(date -u -r "$MARK" '+%F %H:%M')Z"
  fi
fi

ENVF=$(mktemp "$STATE/.alert-env.XXXXXX")
BODYF=$(mktemp "$STATE/.alert-body.XXXXXX")
chmod 600 "$ENVF"
trap 'shred -u "$ENVF" 2>/dev/null || rm -f "$ENVF"; rm -f "$BODYF"' EXIT
if [ "$BODY" = - ]; then cat > "$BODYF"; else cp "$BODY" "$BODYF"; fi
chmod 644 "$BODYF"

undelivered() {
  printf '%s\t%s\t%s\n' "$(date -u '+%F %T')Z" "$SUBJ" "$1" >> "$STATE/undelivered"
  echo "alert: $1"
  exit 1
}

# Only what ops-alert needs: the store's key, the database it lives in, and
# the mail settings compose carries.
docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$OAUTH" \
  | grep -E '^(KUN_CONFIG_KEK|KUN_PG_[A-Z_]+|KUN_VISUAL_NOVEL_EMAIL_[A-Z_]+)=' > "$ENVF" \
  || undelivered "could not read the environment of $OAUTH"
IMG=$(docker image inspect --format '{{index .RepoDigests 0}}' ghcr.io/next-moe/infra-tools:latest 2>/dev/null) \
  || IMG=ghcr.io/next-moe/infra-tools:latest

if docker run --rm --network "container:$PG" --env-file "$ENVF" -e KUN_PG_HOST=127.0.0.1 \
  -v "$BODYF:/w/body:ro" "$IMG" ops-alert -to "$TO" -subject "$SUBJ" -body /w/body -note "$NOTE"; then
  printf '%s\n0\n' "$DIGEST" > "$MARK"
else
  undelivered "ops-alert failed (exit $?)"
fi
