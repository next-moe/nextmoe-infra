package keys

import "api/internal/platform/settings"

var AppKungalHomeTopicCount = settings.Int(settings.Meta{
	Name:   "app.kungal.home_topic_count",
	DescEN: "How many topics the kungal app lists on its home page.",
	DescZH: "鲲 Galgame App 首页显示的话题条数。",
	Min:    settings.F(5),
	Max:    settings.F(100),
}, 20)

var AppKungalHomeWorkRows = settings.Int(settings.Meta{
	Name:   "app.kungal.home_work_rows",
	DescEN: "How many rows the work grid on the kungal app's home page has.",
	DescZH: "鲲 Galgame App 首页作品网格的行数。",
	Min:    settings.F(1),
	Max:    settings.F(4),
}, 2)

var AppKungalHomeRatingCount = settings.Int(settings.Meta{
	Name:   "app.kungal.home_rating_count",
	DescEN: "How many player ratings the kungal app shows on its home page; 0 hides the section.",
	DescZH: "鲲 Galgame App 首页显示的玩家评分条数,0 = 隐藏该区块。",
	Min:    settings.F(0),
	Max:    settings.F(20),
}, 6)

var kungalApp = []settings.Entry{
	AppKungalHomeTopicCount,
	AppKungalHomeWorkRows,
	AppKungalHomeRatingCount,
}

var appKeysByClient = map[string][]settings.Entry{
	"kungal-app":         kungalApp,
	"kungal-app-desktop": kungalApp,
}

var appDomain = settings.Domain{
	Name:    "app",
	TitleZH: "原生 App",
	Keys:    kungalApp,
}

func AppKeys(clientID string) []settings.Entry { return appKeysByClient[clientID] }

func AllAppKeys() []settings.Entry { return appDomain.Keys }
