package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateReport(t *testing.T) {
	base := PlaytimeReport{ActorUID: 7, WorkID: 1, ClientID: "kurumi", Minutes: 600}

	cases := []struct {
		name string
		mut  func(*PlaytimeReport)
		want error
	}{
		{"a well-formed report", func(*PlaytimeReport) {}, nil},
		{"zero minutes", func(r *PlaytimeReport) { r.Minutes = 0 }, nil},
		{"exactly the ceiling", func(r *PlaytimeReport) { r.Minutes = model.PlaytimeMinutesMax }, nil},
		{"past the ceiling", func(r *PlaytimeReport) { r.Minutes = model.PlaytimeMinutesMax + 1 }, ErrPlaytimeMinutesRange},
		{"negative minutes", func(r *PlaytimeReport) { r.Minutes = -1 }, ErrPlaytimeMinutesRange},
		{"no user", func(r *PlaytimeReport) { r.ActorUID = 0 }, ErrPlaytimeActorRequired},
		{"no client", func(r *PlaytimeReport) { r.ClientID = "" }, ErrPlaytimeClientRequired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := base
			c.mut(&r)
			err := validateReport(r)
			if c.want == nil {
				require.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, c.want)
		})
	}
}
