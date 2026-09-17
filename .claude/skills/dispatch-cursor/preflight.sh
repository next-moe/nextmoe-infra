#!/usr/bin/env bash
# The executor's first command. dispatch.sh reads this line back out of the stream as proof that
# the shell it was given is fenced; see SKILL.md section 4.
reach() { timeout 5 bash -c "exec 3<>/dev/tcp/$1/$2" 2>/dev/null; }

net=blocked
reach 1.1.1.1 443 && net=open

loopback=private
if [ -z "${DISPATCH_LOOPBACK_PORT:-}" ]; then
  loopback=unknown
elif reach 127.0.0.1 "$DISPATCH_LOOPBACK_PORT"; then
  loopback=host
fi

echo "dispatch-preflight: sandbox=${CURSOR_SANDBOX:-none} net=$net loopback=$loopback"
echo "dispatch-cache: ${GOCACHE:-$(go env GOCACHE 2>/dev/null)}"
