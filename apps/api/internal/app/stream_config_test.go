package app

import "testing"

func TestFiberConfigDoesNotStream(t *testing.T) {
	cfg := FiberConfig("kun-test")
	if cfg.StreamRequestBody || cfg.DisablePreParseMultipartForm {
		t.Fatalf("FiberConfig enabled streaming: StreamRequestBody=%v DisablePreParseMultipartForm=%v",
			cfg.StreamRequestBody, cfg.DisablePreParseMultipartForm)
	}
}

func TestStreamingFiberConfig(t *testing.T) {
	cfg := StreamingFiberConfig("kun-telemetry")
	if !cfg.StreamRequestBody || !cfg.DisablePreParseMultipartForm {
		t.Fatalf("StreamingFiberConfig: StreamRequestBody=%v DisablePreParseMultipartForm=%v",
			cfg.StreamRequestBody, cfg.DisablePreParseMultipartForm)
	}
}
