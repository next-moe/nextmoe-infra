package service

import (
	"context"
	"testing"

	"api/internal/platform/ai/model"
	"api/internal/platform/ai/upstream"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
)

func TestUserProfileGoesStraightToTheLLMWithItsOwnPrompt(t *testing.T) {
	settings.Override(t, keys.AIForceEscalate, []string{})
	settings.Override(t, keys.AINegativeSampleRate, 0)
	cleanTables(t)
	omni := &fakeOmni{configured: true, model: omniModel, result: upstream.OmniResult{
		Categories: map[string]bool{}, CategoryScores: map[string]float64{}, Channel: omniModel,
	}}
	llm := &fakeUpstream{configured: true, model: "glm", result: upstream.ChatResult{
		Content: `{"flagged": true, "categories": ["sex_ad"], "score": 0.95}`,
		Channel: "glm",
	}}
	s := NewModerationService(testDB, omni, llm, ModerationOptions{})

	res, err := s.Moderate(context.Background(), ModerateParams{
		Site: "trust", SubjectSite: "account", SubjectKind: "user_profile", Text: "用户名: 台灣學生妹TGxs9805",
	})
	if err != nil {
		t.Fatalf("Moderate: %v", err)
	}
	if !res.Flagged || res.Channel != "glm" {
		t.Fatalf("want the LLM verdict, got flagged=%v channel=%q", res.Flagged, res.Channel)
	}
	if omni.calls != 0 {
		t.Fatalf("omni was called %d times for a profile", omni.calls)
	}
	if llm.lastSystem != ProfileModeratePrompt {
		t.Fatalf("profile prompt not used, got %q", llm.lastSystem)
	}
	if rows := usageRows(t, "trust", model.RouteModerateText); len(rows) != 1 {
		t.Fatalf("want 1 metered row (LLM only), got %d", len(rows))
	}
}
