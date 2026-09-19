package sourcedate

import "time"

type State int8

const (
	Unknown State = iota
	Dated
	TBA
)

type Verdict struct {
	State State
	Y     int16
	M, D  *int16
}

const minYear = 1950

func MaxYear(now time.Time) int { return now.Year() + 3 }
