package llm

import "api/pkg/config"

func BootstrapFromConfig(cfg *config.Config) Bootstrap {
	return Bootstrap{
		UpstreamBaseURL: cfg.AIUpstream.BaseURL, UpstreamKeyEnv: "KUN_AI_UPSTREAM_TOKEN", UpstreamModel: cfg.AIUpstream.Model,
		OmniBaseURL: cfg.AIOmni.BaseURL, OmniKeyEnv: "KUN_AI_OMNI_TOKEN", OmniModel: cfg.AIOmni.Model,
	}
}
