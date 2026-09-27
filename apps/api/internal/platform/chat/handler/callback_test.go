package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"api/internal/platform/chat/model"
	"api/internal/platform/chat/service"
	"api/pkg/trustclient"
)

func TestTrustCallbackRoute(t *testing.T) {
	app := newApp(t)
	svc := service.New(testDB, service.Options{Users: users{1: {ID: 1, CreatedAt: time.Unix(0, 0)}, 2: {ID: 2, CreatedAt: time.Unix(0, 0)}}, Relationships: everyoneFollows{}})
	const secret = "chat-callback-secret"
	app.Post("/trust/callback", TrustCallback(secret, svc))

	_, conv, _ := call(t, app, http.MethodPut, "/v2/chat/direct/2", "write:1", "")
	_, msg, _ := call(t, app, http.MethodPost, "/v2/chat/conversations/"+conv["id"].(string)+"/messages", "write:1", `{"text":"offensive"}`)
	id := msg["id"].(string)

	post := func(sig string, body []byte) int {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		if sig == "" {
			sig = trustclient.SignCallback(secret, ts, body)
		}
		req := httptest.NewRequest(http.MethodPost, "/trust/callback", bytes.NewReader(body))
		req.Header.Set("X-Trust-Timestamp", ts)
		req.Header.Set("X-Trust-Signature", sig)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode
	}
	body := []byte(`{"disposition_id":5,"subject_kind":"chat_message","subject_id":"` + id + `","action":2,"reason_code":"x"}`)
	if got := post("deadbeef", body); got != http.StatusUnauthorized {
		t.Fatalf("bad signature: %d", got)
	}
	var row model.ChatMessage
	testDB.Where("id = ?", id).Take(&row)
	if row.DeletedAt != nil {
		t.Fatal("an unsigned callback must not erase anything")
	}
	if got := post("", body); got != http.StatusOK {
		t.Fatalf("signed callback: %d", got)
	}
	testDB.Where("id = ?", id).Take(&row)
	if row.DeletedAt == nil {
		t.Fatal("a signed remove must erase the message")
	}
	if got := post("", []byte(`{"subject_kind":"chat_message","subject_id":"1","action":9}`)); got != http.StatusOK {
		t.Fatalf("an unsupported action is acknowledged, not retried: %d", got)
	}
}
