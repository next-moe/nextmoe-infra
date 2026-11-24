package registry

// Program is an ingest task fanin accepts rows from (plan 05 §9). Its medium
// is the only source of a new work's medium: no page is evidence for it.
type Program struct {
	Key    string `json:"key"`
	Medium string `json:"medium"`
	Doc    string `json:"doc"`
}

// A program's mode (D6): in review every verified row waits for a reviewer,
// in auto it is written, and a stopped program's rows stay in the pool.
const (
	ModeReview  = "review"
	ModeAuto    = "auto"
	ModeStopped = "stopped"
)

var Modes = []string{ModeReview, ModeAuto, ModeStopped}

var Programs = []Program{
	{Key: "ln-pilot", Medium: "novel", Doc: "Japanese bunko light novels of the pilot labels and their official Chinese editions (D4)."},
}

func ProgramByKey(key string) (Program, bool) {
	for _, p := range Programs {
		if p.Key == key {
			return p, true
		}
	}
	return Program{}, false
}
