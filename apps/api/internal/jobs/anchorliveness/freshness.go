package anchorliveness

// a crawl that died half way looks like mass deletion
const egFreshPercent int64 = 97

func FreshEnough(fresh, total int64) bool {
	if total <= 0 {
		return false
	}
	return fresh*100 >= total*egFreshPercent
}
