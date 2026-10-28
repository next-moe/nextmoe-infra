package llm

import (
	"os"

	"api/pkg/config"
)

func BootstrapFromConfig(cfg *config.Config) Bootstrap {
	return Bootstrap{
		UpstreamBaseURL: cfg.AIUpstream.BaseURL, UpstreamKeyEnv: "KUN_AI_UPSTREAM_TOKEN", UpstreamModel: os.Getenv("KUN_AI_UPSTREAM_MODEL"),
		OmniBaseURL: cfg.AIOmni.BaseURL, OmniKeyEnv: "KUN_AI_OMNI_TOKEN", OmniModel: os.Getenv("KUN_AI_OMNI_MODEL"),
	}
}
