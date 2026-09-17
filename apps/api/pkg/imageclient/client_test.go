package imageclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetaBatchDecodesSexualAndKeepsUngradedNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"metas":{
			"aa":{"width":800,"height":600,"thumbhash":"th","sexual":2},
			"bb":{"width":100,"height":100,"thumbhash":"th2","sexual":0},
			"cc":{"width":10,"height":10}
		}}}`))
	}))
	defer srv.Close()

	metas, err := New(Config{BaseURL: srv.URL}).MetaBatch(context.Background(), []string{"aa", "bb", "cc"})
	if err != nil {
		t.Fatalf("meta-batch: %v", err)
	}
	if metas["aa"].Sexual == nil || *metas["aa"].Sexual != 2 {
		t.Fatalf("aa sexual = %v, want 2", metas["aa"].Sexual)
	}
	if metas["bb"].Sexual == nil || *metas["bb"].Sexual != 0 {
		t.Fatalf("bb sexual = %v, want an explicit 0", metas["bb"].Sexual)
	}
	if metas["cc"].Sexual != nil {
		t.Fatalf("cc sexual = %v, want nil for an ungraded image", *metas["cc"].Sexual)
	}
}

func TestClassifyErrorAndIsPermanent(t *testing.T) {
	if IsPermanent(nil) {
		t.Fatal("IsPermanent(nil) = true")
	}
	cases := []struct {
		code      int
		sentinel  error
		permanent bool
	}{
		{80010, ErrDecodeFailed, true},
		{80009, ErrMIMEDenied, true},
		{60002, ErrModerationRejected, true},
		{80008, ErrQuotaExceeded, false},
		{80001, ErrUnauthorized, false},
		{80012, nil, false},
	}
	for _, tc := range cases {
		err := classifyError(&Error{Code: tc.code, Message: "x"})
		if tc.sentinel != nil && !errors.Is(err, tc.sentinel) {
			t.Errorf("code %d: got %v, want wrap of %v", tc.code, err, tc.sentinel)
		}
		if tc.sentinel == nil {
			var raw *Error
			if !errors.As(err, &raw) || raw.Code != tc.code {
				t.Errorf("code %d: got %v, want raw *Error", tc.code, err)
			}
		}
		if got := IsPermanent(err); got != tc.permanent {
			t.Errorf("code %d IsPermanent = %v, want %v", tc.code, got, tc.permanent)
		}
	}
}
