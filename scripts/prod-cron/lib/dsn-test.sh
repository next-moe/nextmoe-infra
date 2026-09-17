#!/bin/sh
# Offline check over every job in scripts/prod-cron: no database password on a
# tool's argv. Each DSNSH/MTDSN snippet is run with a sentinel password in its
# environment; the snippet must export it as PGPASSWORD and keep it out of
# every DSN it builds.
set -u
DIR=${PROD_CRON_DIR:-$(cd "$(dirname "$0")/.." && pwd)}
SENTINEL=sentinel-pw-do-not-print
fails=0
snippets=0

fail() {
  echo "FAIL $1"
  fails=$((fails + 1))
}

dump=$(mktemp)
trap 'rm -f "$dump"' EXIT
cat > "$dump" <<'EOF'
printf 'PGPASSWORD=%s\n' "${PGPASSWORD:-}"
for v in CAT EG DL HL GC FORUM DSN B IMGDSN AIDSN CATDSN; do
  eval "val=\${$v:-}"
  printf '%s=%s\n' "$v" "$val"
done
EOF

for run in "$DIR"/*/run.sh; do
  job=$(basename "$(dirname "$run")")
  if grep -n 'password=' "$run" | grep -qv '^[0-9]*:[[:space:]]*#'; then
    fail "$job: a DSN names password="
  fi
  for var in DSNSH MTDSN; do
    line=$(grep "^$var=" "$run") || continue
    snippets=$((snippets + 1))
    # shellcheck disable=SC2034
    PGHOST_C=h
    snippet=
    eval "$line"
    eval "snippet=\$$var"
    out=$(env -i PATH="$PATH" \
      KUN_CATALOG_PG_USER=u KUN_CATALOG_PG_PASSWORD="$SENTINEL" \
      KUN_PG_USER=u KUN_PG_PASSWORD="$SENTINEL" KUN_PG_HOST=h KUN_PG_PORT=5432 \
      KUN_CATALOG_PG_DATABASE=kun_catalog \
      sh -c "$snippet; . '$dump'")
    if ! printf '%s\n' "$out" | grep -qx "PGPASSWORD=$SENTINEL"; then
      fail "$job $var: PGPASSWORD is not exported"
    fi
    if printf '%s\n' "$out" | grep -v '^PGPASSWORD=' | grep -q "$SENTINEL"; then
      fail "$job $var: the password reaches a DSN"
    fi
    if ! printf '%s\n' "$out" | grep -v '^PGPASSWORD=' | grep -q 'user=u'; then
      fail "$job $var: the snippet built no DSN"
    fi
  done
done

if [ "$snippets" -eq 0 ]; then
  fail "no DSNSH/MTDSN snippet found under $DIR"
fi
echo "checked $snippets snippets, $fails failures"
[ "$fails" -eq 0 ]
