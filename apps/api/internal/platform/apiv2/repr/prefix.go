package repr

import "regexp"

type Prefix struct {
	Prefix  string
	Use     string
	Pattern *regexp.Regexp
}

var Prefixes = []Prefix{
	{Prefix: "nmk_live_", Use: "application key (live)", Pattern: regexp.MustCompile(`^nmk_live_[A-Za-z0-9]{28}$`)},
	{Prefix: "nmk_test_", Use: "application key (test)", Pattern: regexp.MustCompile(`^nmk_test_[A-Za-z0-9]{28}$`)},
	{Prefix: "req_", Use: "request id", Pattern: regexp.MustCompile(`^req_[0-9A-HJKMNP-TV-Z]{26}$`)},
	{Prefix: "cur_", Use: "pagination cursor", Pattern: regexp.MustCompile(`^cur_[A-Za-z0-9._~-]+$`)},
}
