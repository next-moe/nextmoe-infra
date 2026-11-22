package registry

// Source is a place evidence may come from. Fanin grades every value by its
// evidence's source here and ignores the tier a pool row claims (plan 05 §15).
type Source struct {
	Key      string   `json:"key"`
	Kind     string   `json:"kind" enum:"record,host"`
	Hosts    []string `json:"hosts,omitempty"`
	Tier     Tier     `json:"tier,omitempty"`
	Excluded bool     `json:"excluded,omitempty"`
	// MinDelaySeconds is the least gap between two requests to the source,
	// never below its robots.txt Crawl-delay.
	MinDelaySeconds int    `json:"min_delay_seconds,omitempty"`
	Note            string `json:"note"`
}

// Sources follows the due diligence of 2026-11-22 (plan 13-sources). An
// unlisted host is excluded: a value from it is refused, never graded.
var Sources = []Source{
	{Key: "ndl", Kind: "record", Tier: TierRegistry, MinDelaySeconds: 3, Note: "NDL Search, NDL bibliographic data; credit 出典：国立国会図書館書誌データ"},
	{Key: "jpro", Kind: "record", Tier: TierRegistry, MinDelaySeconds: 3, Note: "JPRO data relayed by NDL Search; non-commercial use (D25)"},
	{Key: "kadokawa", Kind: "host", Hosts: []string{"kadokawa.co.jp"}, Tier: TierOfficial, MinDelaySeconds: 20, Note: "facts only; Crawl-delay 20"},
	{Key: "shogakukan", Kind: "host", Hosts: []string{"shogakukan.co.jp", "gagagabunko.jp"}, Tier: TierOfficial, MinDelaySeconds: 10, Note: "facts only; Crawl-delay 10"},
	{Key: "shueisha_dash", Kind: "host", Hosts: []string{"dash.shueisha.co.jp"}, Tier: TierOfficial, MinDelaySeconds: 30, Note: "facts only; low rate; terms not found"},
	{Key: "overlap", Kind: "host", Hosts: []string{"over-lap.co.jp"}, Tier: TierOfficial, MinDelaySeconds: 30, Note: "facts only; low rate; terms not found"},
	{Key: "kadokawa_tw", Kind: "host", Hosts: []string{"kadokawa.com.tw"}, Tier: TierOfficial, MinDelaySeconds: 5, Note: "facts only; Crawl-delay 5; no ISBN on the page"},
	{Key: "chingwin", Kind: "host", Hosts: []string{"ching-win.com.tw"}, Tier: TierOfficial, MinDelaySeconds: 15, Note: "non-commercial with credit 資料提供：青文出版社"},
	{Key: "tongli", Kind: "host", Hosts: []string{"tongli.com.tw"}, Tier: TierOfficial, MinDelaySeconds: 30, Note: "facts only; terms not found"},
	{Key: "kodansha", Kind: "host", Hosts: []string{"kodansha.co.jp"}, Excluded: true, Note: "terms forbid automated collection"},
	{Key: "hobbyjapan", Kind: "host", Hosts: []string{"firecross.jp", "hobbyjapan.co.jp"}, Excluded: true, Note: "WAF challenge; never bypassed"},
	{Key: "stores", Kind: "host", Hosts: []string{"bookwalker.jp", "amazon.co.jp", "books.rakuten.co.jp", "books.com.tw"}, Excluded: true, Note: "store terms forbid scraping or storage"},
	{Key: "hanmoto", Kind: "host", Hosts: []string{"hanmoto.com", "books.or.jp"}, Excluded: true, Note: "robots or terms forbid"},
}

func SourceForRecord(key string) (Source, bool) {
	for _, s := range Sources {
		if s.Kind == "record" && s.Key == key {
			return s, true
		}
	}
	return Source{}, false
}

func SourceForHost(host string) (Source, bool) {
	for _, s := range Sources {
		for _, h := range s.Hosts {
			if host == h || len(host) > len(h) && host[len(host)-len(h)-1:] == "."+h {
				return s, true
			}
		}
	}
	return Source{}, false
}
