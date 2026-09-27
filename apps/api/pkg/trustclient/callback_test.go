package trustclient

import (
	"strconv"
	"testing"
	"time"
)

func TestVerifyCallback(t *testing.T) {
	const secret = "trust-callback-secret"
	body := []byte(`{"disposition_id":1,"action":1}`)
	now := time.Now()
	ts := strconv.FormatInt(now.Unix(), 10)
	sig := SignCallback(secret, ts, body)

	if !VerifyCallback(secret, ts, sig, body, now) {
		t.Fatal("a valid signature must verify")
	}
	if VerifyCallback(secret, ts, "deadbeef", body, now) {
		t.Fatal("a bad signature must fail")
	}
	if VerifyCallback(secret, ts, sig, []byte(`{"disposition_id":1,"action":2}`), now) {
		t.Fatal("a signature over another body must fail")
	}
	stale := strconv.FormatInt(now.Add(-10*time.Minute).Unix(), 10)
	if VerifyCallback(secret, stale, SignCallback(secret, stale, body), body, now) {
		t.Fatal("a stale timestamp must fail")
	}
	if VerifyCallback("", ts, sig, body, now) {
		t.Fatal("an empty secret must fail closed")
	}
	if VerifyCallback(secret, "", "", body, now) {
		t.Fatal("missing headers must fail")
	}
}
