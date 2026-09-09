package keys_test

import (
	"regexp"
	"strings"
	"testing"

	"api/internal/platform/settings/keys"
)

var goldenNames = []string{
	"platform.read_only",
	"platform.notice",
	"auth.verification_code_ttl_minutes",
	"auth.ip_rate_per_minute",
	"auth.token_endpoint_rate_per_minute",
	"auth.strict_rate_per_minute",
	"auth.allowed_email_domains",
	"auth.federation_providers",
	"auth.verification_resend_cooldown_seconds",
	"auth.register_gift_points",
	"image.upload_enabled",
	"image.gc_cold_after_days",
	"image.gc_softdelete_after_days",
	"image.gc_harddelete_after_days",
	"image.gc_max_per_run",
	"artifact.upload_enabled",
	"artifact.multipart_threshold_bytes",
	"artifact.part_size_bytes",
	"artifact.presign_upload_ttl_seconds",
	"artifact.presign_download_ttl_seconds",
	"artifact.orphan_ttl_hours",
	"artifact.softdelete_ttl_hours",
	"artifact.reclaim_min_idle_seconds",
	"artifact.gc_max_per_run",
	"trust.scan_enabled",
	"trust.check_enabled",
	"trust.scan_mode",
	"trust.scan_sample_rate",
	"trust.report_rate_window_minutes",
	"trust.report_rate_max_per_window",
	"trust.aggregate_threshold",
	"trust.new_account_age_days",
	"trust.new_account_reporter_weight",
	"trust.policy_cache_ttl_seconds",
	"trust.term_cache_ttl_seconds",
	"ai.escalate_threshold",
	"ai.negative_sample_rate",
	"ai.force_escalate",
	"ai.moderate_max_tokens",
	"ai.upstream_model",
	"ai.omni_model",
	"store.link_quota_per_client",
	"store.price_enabled",
	"store.price_user_agent",
	"store.price_steam_regions",
	"store.price_dlsite_currencies",
	"store.price_wait_on_miss_ms",
	"store.price_fresh_for_hours",
	"apiv2.default_rate_per_minute",
	"apiv2.default_quota_per_day",
	"apiv2.auth_fail_per_minute",
	"apiv2.auth_fail_block_seconds",
	"catalog.totals_cache_ttl_seconds",
	"catalog.merge_cooling_off_hours",
	"community.sandbox_max_links",
	"community.sandbox_max_images",
	"community.sandbox_max_mentions",
	"community.sandbox_max_topics_per_day",
	"community.sandbox_max_replies_per_day",
	"community.sandbox_window_hours",
	"community.flag_hide_threshold",
	"developer.credential_cache_ttl_seconds",
	"developer.credential_cache_negative_ttl_seconds",
	"jobs.image_gc.enabled",
	"jobs.image_gc.schedule",
	"jobs.galgame_image_refping.enabled",
	"jobs.galgame_image_refping.schedule",
	"jobs.catalog_image_refping.enabled",
	"jobs.catalog_image_refping.schedule",
	"jobs.news_image_refping.enabled",
	"jobs.news_image_refping.schedule",
	"jobs.user_avatar_refping.enabled",
	"jobs.user_avatar_refping.schedule",
	"jobs.image_ref_audit.enabled",
	"jobs.image_ref_audit.schedule",
	"jobs.artifact_gc.enabled",
	"jobs.artifact_gc.schedule",
	"jobs.prune_developer_usage.enabled",
	"jobs.prune_developer_usage.schedule",
	"jobs.ymgal_news_poll.enabled",
	"jobs.ymgal_news_poll.schedule",
	"jobs.ymgal_news_sweep.enabled",
	"jobs.ymgal_news_sweep.schedule",
	"jobs.store_stats_sync.enabled",
	"jobs.store_stats_sync.schedule",
	"jobs.news_moderate.enabled",
	"jobs.news_moderate.schedule",
}

func TestLiveRegistryIsWellFormed(t *testing.T) {
	reg := keys.Live()
	entries := reg.Entries()

	got := make(map[string]bool, len(entries))
	for _, e := range entries {
		m := e.Meta()
		if got[m.Name] {
			t.Errorf("duplicate live key %q", m.Name)
		}
		got[m.Name] = true
		if m.DescEN == "" || m.DescZH == "" {
			t.Errorf("%s: missing description", m.Name)
		}
		if m.Public && (m.DescEN == "" || m.DescZH == "") {
			t.Errorf("%s: Public key is missing a description", m.Name)
		}
		if m.EnvVar != "" && !strings.HasPrefix(m.EnvVar, "KUN_") {
			t.Errorf("%s: EnvVar %q must start with KUN_", m.Name, m.EnvVar)
		}
		if err := e.Validate(e.Default()); err != nil {
			t.Errorf("%s: default failed Validate: %v", m.Name, err)
		}
		if m.Kind == "enum" {
			found := false
			def, _ := e.Default().(string)
			for _, v := range m.Enum {
				if v == def {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s: default %q is not in Enum %v", m.Name, def, m.Enum)
			}
		}
	}

	want := make(map[string]bool, len(goldenNames))
	for _, name := range goldenNames {
		want[name] = true
		if !got[name] {
			t.Errorf("golden key %q is missing from Live()", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("Live() has extra key %q", name)
		}
	}

	envVars := make(map[string]string)
	for _, e := range entries {
		m := e.Meta()
		if m.EnvVar == "" {
			continue
		}
		if other, ok := envVars[m.EnvVar]; ok {
			t.Errorf("EnvVar %q is used by both %q and %q", m.EnvVar, other, m.Name)
		}
		envVars[m.EnvVar] = m.Name
	}

	for _, d := range reg.Domains() {
		prefix := d.Name + "."
		for _, e := range d.Keys {
			if !strings.HasPrefix(e.Meta().Name, prefix) {
				t.Errorf("%s: name %q does not start with %q", d.Name, e.Meta().Name, prefix)
			}
		}
	}
}

func TestJobKeysLookup(t *testing.T) {
	jk, ok := keys.Job("image-gc")
	if !ok {
		t.Fatal(`Job("image-gc") missing`)
	}
	if got := jk.Enabled.Name(); got != "jobs.image_gc.enabled" {
		t.Errorf("enabled name %q, want jobs.image_gc.enabled", got)
	}
	if got := jk.Schedule.Name(); got != "jobs.image_gc.schedule" {
		t.Errorf("schedule name %q, want jobs.image_gc.schedule", got)
	}
	if jk.Enabled.Default() != true {
		t.Errorf("enabled default %v, want true", jk.Enabled.Default())
	}
	if jk.Schedule.Default() != "daily@03:30" {
		t.Errorf("schedule default %v, want daily@03:30", jk.Schedule.Default())
	}
	if _, ok := keys.Job("nope"); ok {
		t.Error(`Job("nope") should be missing`)
	}

	names := keys.JobNames()
	if len(names) != 12 {
		t.Fatalf("JobNames() = %d, want 12", len(names))
	}
	seen := make(map[string]bool, len(names))
	re := regexp.MustCompile(keys.JobSchedulePattern)
	for _, name := range names {
		if seen[name] {
			t.Errorf("duplicate JobNames entry %q", name)
		}
		seen[name] = true
		jk, ok := keys.Job(name)
		if !ok {
			t.Errorf("Job(%q) missing", name)
			continue
		}
		def, _ := jk.Schedule.Default().(string)
		if !re.MatchString(def) {
			t.Errorf("%s default %q does not match JobSchedulePattern", name, def)
		}
	}
}
