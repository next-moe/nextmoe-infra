package anchorliveness

import (
	"fmt"
	"strings"
)

type Opts struct {
	DSN      string
	EGDSN    string
	Source   string
	Apply    bool
	Receipts string
	Lanes    []Lane
}

func ValidateOpts(o Opts) error {
	if strings.TrimSpace(o.DSN) == "" {
		return fmt.Errorf("catalog DSN is required (--dsn); refusing to guess")
	}
	switch o.Source {
	case SourceVNDB, SourceBangumi, SourceEG:
	case "":
		return fmt.Errorf("--source is required (vndb|bangumi|erogamescape)")
	default:
		return fmt.Errorf("unknown --source %q (want vndb|bangumi|erogamescape)", o.Source)
	}
	eg := strings.TrimSpace(o.EGDSN)
	if o.Source == SourceEG {
		if eg == "" {
			return fmt.Errorf("--eg-dsn is required when --source erogamescape")
		}
	} else if eg != "" {
		return fmt.Errorf("--eg-dsn is only valid with --source erogamescape")
	}
	return nil
}
