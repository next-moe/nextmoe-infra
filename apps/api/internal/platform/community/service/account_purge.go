package service

import (
	"context"
	"fmt"

	"api/internal/platform/community/model"
)

// Every tenant's author_id is the NextMoe uid (checked on prod 2026-09-25:
// all 5,673 site/author pairs name an existing platform user), so one uid
// purges the account on every tenant. The sites come from every table the
// author purge scopes by site, not from community_thread alone: a reply
// delivered through one tenant onto another tenant's thread records the
// delivering site. community_trust has no site and is dropped once; flags the
// account filed stay, as moderation records.
func (s *PostService) PurgeAccount(ctx context.Context, uid int64) error {
	var sites []string
	if err := s.db.WithContext(ctx).Raw(`
		SELECT site FROM community_thread
		UNION SELECT site FROM community_thread_user WHERE site IS NOT NULL
		UNION SELECT site FROM community_anchor_user
		UNION SELECT site FROM community_notification
		UNION SELECT site FROM community_event
		ORDER BY site`).Scan(&sites).Error; err != nil {
		return err
	}
	for _, site := range sites {
		if _, err := s.PurgeAuthor(ctx, site, uid); err != nil {
			return fmt.Errorf("site %s: %w", site, err)
		}
	}
	return s.db.WithContext(ctx).Where("user_id = ?", uid).Delete(&model.CommunityTrust{}).Error
}
