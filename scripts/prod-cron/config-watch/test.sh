#!/bin/sh
# Offline harness for run.sh beside it: fake docker / flock / alert on PATH, so
# it never reaches Docker or the database.
#
#   sh scripts/prod-cron/config-watch/test.sh
set -eu

RUN_SH="$(cd "$(dirname "$0")" && pwd)/run.sh"
FAILED=0

tstart() { T_ID=$1; T_OK=1; T_MSG=""; }
fail() { T_OK=0; T_MSG="$T_MSG $*"; }
tend() {
  if [ "$T_OK" = 1 ]; then echo "T$T_ID PASS"; else echo "T$T_ID FAIL:$T_MSG"; FAILED=1; fi
}

TD=$(mktemp -d); trap 'rm -rf "$TD"' EXIT
mkdir -p "$TD/bin" "$TD/base" "$TD/ctl"
cat > "$TD/bin/docker" <<'FAKE'
#!/bin/sh
CTL=${FAKE_CTL:?FAKE_CTL unset — refusing to reach a real docker}
case "$*" in
  "exec "*" psql "*) cat "$CTL/snapshot" ;;
  *) echo "fake docker: unexpected $*" >&2; exit 99 ;;
esac
FAKE
printf '#!/bin/sh\nexit 0\n' > "$TD/bin/flock"
cat > "$TD/alert.sh" <<'FAKE'
#!/bin/sh
echo "$1 digest=${ALERT_DIGEST:-}" >> "${FAKE_CTL:?}/alerts"
cp "$2" "$FAKE_CTL/alert-body"
FAKE
chmod +x "$TD/bin/"* "$TD/alert.sh"

run() {
  rm -f "$TD/ctl/alerts"
  FAKE_CTL="$TD/ctl" PATH="$TD/bin:$PATH" CONFIG_WATCH_BASE="$TD/base" CONFIG_WATCH_ALERT="$TD/alert.sh" sh "$RUN_SH"
}
snapshot() { printf '%s\n' "$@" > "$TD/ctl/snapshot"; }
alerts() { cat "$TD/ctl/alerts" 2>/dev/null || true; }

tstart 1
snapshot "agent|KUN_AGENT_CLIENT_ID|set" "agent|KUN_CHAT_TRUST_CLIENT_ID|unset" "chat|KUN_CHAT_TRUST_CLIENT_ID|unset"
run || fail "exit $?"
[ -z "$(alerts)" ] || fail "first run alerted: $(alerts)"
grep -qx 'agent|KUN_AGENT_CLIENT_ID' "$TD/base/state/seen-set" || fail "set pair not remembered"
grep -q 'TRUST' "$TD/base/state/seen-set" && fail "a never-set pair was remembered"
[ -f "$TD/base/state/last-success" ] || fail "no success stamp"
tend

tstart 2
snapshot "agent|KUN_AGENT_CLIENT_ID|unset" "chat|KUN_CHAT_TRUST_CLIENT_ID|unset"
run || fail "exit $?"
alerts | grep -q '^\[CONFIG\] 1 variable(s) lost their value digest=.' || fail "no [CONFIG] alert: $(alerts)"
grep -qx 'agent|KUN_AGENT_CLIENT_ID' "$TD/ctl/alert-body" || fail "lost pair not in the body"
tend

tstart 3
run || fail "exit $?"
alerts | grep -q '^\[CONFIG\]' || fail "a lost pair stopped being reported while still unset"
tend

tstart 4
snapshot "agent|KUN_AGENT_CLIENT_ID|set"
run || fail "exit $?"
[ -z "$(alerts)" ] || fail "alerted after the value came back: $(alerts)"
tend

tstart 5
grep -v 'KUN_AGENT_CLIENT_ID' "$TD/base/state/seen-set" > "$TD/s" || true; mv "$TD/s" "$TD/base/state/seen-set"
snapshot "agent|KUN_AGENT_CLIENT_ID|unset"
run || fail "exit $?"
[ -z "$(alerts)" ] || fail "a pair removed from seen-set still alerted"
tend

tstart 6
: > "$TD/ctl/snapshot"
if run; then fail "an empty snapshot passed"; fi
alerts | grep -q '^\[FAIL\] config watch' || fail "no [FAIL] alert on an empty snapshot"
tend

exit "$FAILED"
