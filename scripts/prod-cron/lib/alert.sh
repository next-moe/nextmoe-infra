#!/bin/sh
# Shared alert sender for the root-owned maintenance crons.
#
#   /root/lib/alert.sh "<subject>" <body-file|->
#
# Pulls SMTP credentials from the live oauth container's env, hands them to
# alert.py as a 0600 file, and shreds it. Values never reach a command line,
# a log, or an exported variable.
#
# Exits non-zero if the mail could not be sent. Callers should tolerate that
# (an unsendable alert must not turn a warning into a failed job) — the
# watchdog is the backstop for an alert path that is itself broken.
set -eu

SUBJ=${1:?subject required}
BODY=${2:--}
TO=${ALERT_TO:-yuyuyukunkunkun@gmail.com}
OAUTH=${ALERT_OAUTH_CONTAINER:-kun-visual-novel-infra-vqvqbc-oauth-1}

ENVF=$(mktemp /root/lib/.alert-env.XXXXXX)
chmod 600 "$ENVF"
trap 'shred -u "$ENVF" 2>/dev/null || rm -f "$ENVF"' EXIT

docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$OAUTH" \
  | grep -E '^KUN_VISUAL_NOVEL_EMAIL_' > "$ENVF" || {
    echo "alert: could not read SMTP config from $OAUTH"; exit 1; }

# Not `exec`: that would replace this shell and the EXIT trap would never run,
# leaving the credential file on disk.
python3 /root/lib/alert.py "$ENVF" "$SUBJ" "$BODY" "$TO"
