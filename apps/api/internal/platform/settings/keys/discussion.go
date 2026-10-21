package keys

import "api/internal/platform/settings"

var CatalogDiscussionPostsPerHour = settings.Int(settings.Meta{
	Name:   "catalog.discussion_posts_per_hour",
	DescEN: "How many discussion posts one account may write per hour under edit proposals and merge, split or deletion requests.",
	DescZH: "每个账号每小时可在编辑提案及合并、拆分、删除请求讨论区发表的留言数。",
	Min:    settings.F(1),
	Max:    settings.F(1000),
}, 30)
