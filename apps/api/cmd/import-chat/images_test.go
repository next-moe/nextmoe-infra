package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"api/pkg/imageclient"
)

func TestRehostFetchesTheBytesFromTheCDN(t *testing.T) {
	const old = "7da81a4affe68037b3972c4447d33be7c5380f371f5e00e596e1a0a821fafc80"
	var uploaded, preset string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cdn/7d/a8/" + old + ".webp":
			_, _ = w.Write([]byte("webp bytes"))
		case "/image/" + old:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":40100,"message":"unauthorized"}`))
		case "/image/upload":
			f, _, err := r.FormFile("file")
			if err != nil {
				t.Errorf("upload without a file: %v", err)
				return
			}
			b, _ := io.ReadAll(f)
			uploaded, preset = string(b), r.FormValue("preset")
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"hash":"new","width":450,"height":450}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cli := imageclient.New(imageclient.Config{BaseURL: srv.URL, CDNBase: srv.URL + "/cdn", ClientID: "chat", ClientSecret: "s"})
	out, failed := rehost(context.Background(), cli, map[imageRef]bool{{Hash: old}: true})

	if failed != 0 || uploaded != "webp bytes" || preset != "message" {
		t.Fatalf("failed=%d uploaded=%q preset=%q", failed, uploaded, preset)
	}
	if m := out[imageRef{Hash: old}]; m.ImageHash != "new" || m.Width != 450 {
		t.Fatalf("media = %+v", m)
	}
}
