package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"api/internal/platform/community/service"
)

func TestListReview_CarriesMatchedTermsAsAnArray(t *testing.T) {
	cleanTables(t)
	s := &Server{review: service.NewReviewService(testDB, service.NoopSink{})}
	if err := testDB.Exec(`
		INSERT INTO community_review_item (site, post_id, source, status, matched_terms) VALUES
		  ('letmoe', 1, 2, 0, '["第一次"]'),
		  ('letmoe', 2, 0, 0, DEFAULT)`).Error; err != nil {
		t.Fatalf("seed review items: %v", err)
	}

	out, err := s.listReview(clientCtx("letmoe"), &listReviewInput{Source: -1})
	if err != nil {
		t.Fatalf("listReview: %v", err)
	}
	raw, err := json.Marshal(out.Body.Data.Items)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, `"post_id":1,`) || !strings.Contains(body, `"matched_terms":["第一次"]`) {
		t.Fatalf("the suspect_words item must name its matched terms: %s", body)
	}
	if !strings.Contains(body, `"matched_terms":[]`) || strings.Contains(body, `"matched_terms":null`) {
		t.Fatalf("an item without matched terms must answer [], which the spec declares non-null: %s", body)
	}
}
