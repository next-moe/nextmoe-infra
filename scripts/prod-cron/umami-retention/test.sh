#!/bin/sh
# Harness for run.sh beside it: a fake docker hands the SQL to psql against
# TEST_DATABASE_DSN, seeded with umami's tables, so the deletes run for real.
#
#   TEST_DATABASE_DSN=... sh scripts/prod-cron/umami-retention/test.sh
set -eu
: "${TEST_DATABASE_DSN:?set TEST_DATABASE_DSN to a throwaway database}"

RUN_SH="$(cd "$(dirname "$0")" && pwd)/run.sh"
FAILED=0

tstart() { T_ID=$1; T_OK=1; T_MSG=""; }
fail() { T_OK=0; T_MSG="$T_MSG $*"; }
tend() {
  if [ "$T_OK" = 1 ]; then echo "T$T_ID PASS"; else echo "T$T_ID FAIL:$T_MSG"; FAILED=1; fi
}
q() { PGOPTIONS='-c client_min_messages=warning' psql "$TEST_DATABASE_DSN" -X -q -At -v ON_ERROR_STOP=1 -c "$1"; }

seed() {
  PGOPTIONS='-c client_min_messages=warning' psql "$TEST_DATABASE_DSN" -X -q -v ON_ERROR_STOP=1 <<'SQL'
DROP TABLE IF EXISTS website_event, session, event_data, session_data, revenue;
CREATE TABLE website_event (event_id uuid PRIMARY KEY DEFAULT gen_random_uuid(), website_id uuid, session_id uuid, created_at timestamptz NOT NULL);
CREATE INDEX ON website_event (created_at);
CREATE TABLE session (session_id uuid PRIMARY KEY, website_id uuid, created_at timestamptz NOT NULL);
CREATE TABLE event_data (event_data_id uuid PRIMARY KEY DEFAULT gen_random_uuid(), website_event_id uuid, created_at timestamptz NOT NULL);
CREATE TABLE session_data (session_data_id uuid PRIMARY KEY DEFAULT gen_random_uuid(), session_id uuid, created_at timestamptz NOT NULL);
CREATE TABLE revenue (revenue_id uuid PRIMARY KEY DEFAULT gen_random_uuid(), event_id uuid, created_at timestamptz NOT NULL);
-- old: only events past the cutoff, spread over 30 days
INSERT INTO session VALUES ('00000000-0000-0000-0000-000000000001', NULL, now() - interval '100 days');
INSERT INTO website_event (session_id, created_at)
  SELECT '00000000-0000-0000-0000-000000000001', now() - interval '100 days' + g * interval '1 hour' FROM generate_series(0, 719) g;
-- straddle: started before the cutoff, still has an event inside the window
INSERT INTO session VALUES ('00000000-0000-0000-0000-000000000002', NULL, now() - interval '2 months' - interval '1 day');
INSERT INTO website_event (session_id, created_at) VALUES
  ('00000000-0000-0000-0000-000000000002', now() - interval '2 months' - interval '1 day'),
  ('00000000-0000-0000-0000-000000000002', now() - interval '2 months' + interval '1 hour');
-- recent
INSERT INTO session VALUES ('00000000-0000-0000-0000-000000000003', NULL, now() - interval '3 days');
INSERT INTO website_event (session_id, created_at) VALUES ('00000000-0000-0000-0000-000000000003', now() - interval '3 days');
-- old session that never had an event
INSERT INTO session VALUES ('00000000-0000-0000-0000-000000000004', NULL, now() - interval '90 days');
INSERT INTO event_data (created_at) VALUES (now() - interval '90 days'), (now() - interval '1 day');
INSERT INTO session_data (created_at) VALUES (now() - interval '90 days'), (now() - interval '1 day');
INSERT INTO revenue (created_at) VALUES (now() - interval '90 days'), (now() - interval '1 day');
SQL
}

install_fakes() {
  td=$1
  mkdir -p "$td/bin" "$td/base" "$td/ctl"
  cat > "$td/bin/docker" <<'FAKE'
#!/bin/sh
set -eu
CTL=${FAKE_CTL:?FAKE_CTL unset — refusing to reach a real docker}
[ "$1" = exec ] && [ "$2" = -i ] || { echo "fake docker: unexpected $*" >&2; exit 99; }
echo "$3" > "$CTL/container"
[ "$4" = psql ] || { echo "fake docker: expected psql, got $4" >&2; exit 99; }
shift 4
n=$#; i=0
while [ "$i" -lt "$n" ]; do
  a=$1; shift; i=$((i + 1))
  case "$a" in
    -U|-d) echo "$a $1" >> "$CTL/psql-target"; shift; i=$((i + 1)) ;;
    *) set -- "$@" "$a" ;;
  esac
done
exec psql "$TEST_DATABASE_DSN" "$@"
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
  env PATH="$td/bin:$PATH" FAKE_CTL="$td/ctl" TEST_DATABASE_DSN="$TEST_DATABASE_DSN" \
    UMAMI_RETENTION_BASE="$td/base" UMAMI_RETENTION_ALERT="$td/alert.sh" \
    sh "$RUN_SH" > "$td/ctl/stdout" 2>&1
  echo $? > "$td/ctl/exit"
  set -e
}
exit_is() { [ "$(cat "$1/ctl/exit")" = "$2" ]; }
has_stamp() { [ -f "$1/base/state/last-success" ]; }
log_has() { cat "$1"/base/logs/run-*.log | grep -q "$2"; }

# --- T1: everything past two months goes, a straddling session keeps its live event, the rest stays ---
tstart 1
seed
td=$(mktemp -d); install_fakes "$td"; run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit"): $(tail -3 "$td"/base/logs/run-*.log)"
has_stamp "$td" || fail "no stamp"
[ "$(cat "$td/ctl/container")" = kun-visual-novel-umami-fm1njw-db-1 ] || fail "container $(cat "$td/ctl/container")"
[ "$(tr '\n' ' ' < "$td/ctl/psql-target")" = "-U umami -d umami " ] || fail "psql target $(cat "$td/ctl/psql-target")"
[ "$(q "SELECT count(*) FROM website_event WHERE created_at < now() - interval '2 months'")" = 0 ] || fail "old events left"
[ "$(q "SELECT count(*) FROM website_event")" = 2 ] || fail "events left: $(q "SELECT count(*) FROM website_event")"
[ "$(q "SELECT string_agg(right(session_id::text, 1), ',' ORDER BY session_id) FROM session")" = 2,3 ] \
  || fail "sessions left: $(q "SELECT string_agg(right(session_id::text, 1), ',' ORDER BY session_id) FROM session")"
for t in event_data session_data revenue; do
  [ "$(q "SELECT count(*) FROM $t")" = 1 ] || fail "$t left: $(q "SELECT count(*) FROM $t")"
done
log_has "$td" 'website_event 721$' || fail "event count not logged: $(grep website_event "$td"/base/logs/run-*.log)"
log_has "$td" 'session 2$' || fail "session count not logged"
tend

# --- T2: a second run deletes nothing ---
tstart 2
run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
[ "$(grep -c 'website_event 0$' "$td"/base/logs/run-*.log)" = 1 ] || fail "second run deleted events"
[ "$(q "SELECT count(*) FROM website_event")" = 2 ] || fail "events changed"
tend; rm -rf "$td"

# --- T3: the backlog goes one day per transaction ---
tstart 3
seed
td=$(mktemp -d); install_fakes "$td"
q "CREATE TABLE IF NOT EXISTS del_log (xid xid8, n int); CREATE OR REPLACE FUNCTION log_del() RETURNS trigger LANGUAGE plpgsql AS \$\$ BEGIN INSERT INTO del_log VALUES (pg_current_xact_id(), 1); RETURN OLD; END \$\$; CREATE TRIGGER t BEFORE DELETE ON website_event FOR EACH ROW EXECUTE FUNCTION log_del();" >/dev/null
run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit"): $(tail -3 "$td"/base/logs/run-*.log)"
x=$(q "SELECT count(DISTINCT xid) FROM del_log")
m=$(q "SELECT max(c) FROM (SELECT count(*) c FROM del_log GROUP BY xid) s")
[ "$x" -ge 30 ] || fail "only $x transactions"
[ "$m" -le 24 ] || fail "a transaction deleted $m rows (more than a day)"
q "DROP TABLE del_log; DROP FUNCTION log_del() CASCADE" >/dev/null
tend; rm -rf "$td"

# --- T4: a SQL failure fails the run and alerts without a stamp ---
tstart 4
seed
q "DROP TABLE revenue" >/dev/null
td=$(mktemp -d); install_fakes "$td"; run_job "$td"
exit_is "$td" 0 && fail "exit 0"
has_stamp "$td" && fail "stamp written"
grep -q '^\[FAIL\] umami-retention' "$td/ctl/alerts" 2>/dev/null || fail "no alert"
tend; rm -rf "$td"

# --- T5: a held lock does nothing ---
tstart 5
seed
td=$(mktemp -d); install_fakes "$td"
FLOCK_HOLD_FAIL=1 run_job "$td"
exit_is "$td" 0 || fail "exit $(cat "$td/ctl/exit")"
has_stamp "$td" && fail "stamp written"
[ -f "$td/ctl/container" ] && fail "psql ran"
tend; rm -rf "$td"

q "DROP TABLE IF EXISTS website_event, session, event_data, session_data, revenue" >/dev/null
[ "$FAILED" -eq 0 ]
