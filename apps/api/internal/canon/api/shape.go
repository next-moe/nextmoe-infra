package api

import (
	"strconv"
	"time"

	"api/internal/canon/ent"
)

type Title struct {
	Text string `json:"text"`
	Lang string `json:"lang"`
	Kind string `json:"kind" doc:"official, alternative, romanized or translated."`
}

type Part struct {
	ID         string   `json:"id"`
	ParentID   *string  `json:"parent_id"`
	Kind       string   `json:"kind"`
	Ordinal    *float64 `json:"ordinal"`
	Label      string   `json:"label"`
	Released   *string  `json:"released" doc:"YYYY, YYYY-MM, YYYY-MM-DD or unknown; precision as stated."`
	ReleaseIDs []string `json:"release_ids"`
}

type AliasRef struct {
	ID       string `json:"id" doc:"The person alias id; legacy credit-name ids are alias ids (D19)."`
	Name     string `json:"name"`
	Latin    string `json:"latin"`
	PersonID string `json:"person_id"`
}

type Credit struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	WorkID    *string   `json:"work_id"`
	PartID    *string   `json:"part_id"`
	ReleaseID *string   `json:"release_id"`
	Alias     *AliasRef `json:"alias"`
	OrgID     *string   `json:"org_id"`
}

type BookWork struct {
	Category string `json:"category"`
}

type Work struct {
	ID            string    `json:"id"`
	Medium        string    `json:"medium"`
	Status        string    `json:"status"`
	ContentRating string    `json:"content_rating"`
	Olang         string    `json:"olang"`
	Titles        []Title   `json:"titles"`
	Book          *BookWork `json:"book"`
	Parts         []Part    `json:"parts"`
	Credits       []Credit  `json:"credits"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type BookRelease struct {
	ISBN13      *string `json:"isbn13"`
	JAN         *string `json:"jan"`
	LabelNumber string  `json:"label_number"`
	Pages       *int    `json:"pages"`
	PublisherID *string `json:"publisher_id"`
	LabelID     *string `json:"label_id"`
}

type Content struct {
	WorkID  string  `json:"work_id"`
	PartID  *string `json:"part_id"`
	Portion string  `json:"portion"`
}

type Release struct {
	ID            string       `json:"id"`
	Lang          string       `json:"lang"`
	EditionKind   string       `json:"edition_kind"`
	Format        string       `json:"format"`
	Released      *string      `json:"released"`
	ReleaseStatus string       `json:"release_status"`
	OfficialURL   string       `json:"official_url"`
	Book          *BookRelease `json:"book"`
	Contents      []Content    `json:"contents"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

type Alias struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Latin string `json:"latin"`
	Lang  string `json:"lang"`
}

type Person struct {
	ID             string   `json:"id"`
	PrimaryAliasID *string  `json:"primary_alias_id"`
	Aliases        []Alias  `json:"aliases"`
	Credits        []Credit `json:"credits"`
}

type OrgName struct {
	Text string `json:"text"`
	Lang string `json:"lang"`
}

type Org struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"`
	ParentID *string   `json:"parent_id"`
	Names    []OrgName `json:"names"`
}

func id(n int64) string { return strconv.FormatInt(n, 10) }

func idp(n *int64) *string {
	if n == nil {
		return nil
	}
	s := id(*n)
	return &s
}

func shapeCredit(c *ent.Credit) Credit {
	out := Credit{ID: id(c.ID), Role: c.Role, WorkID: idp(c.WorkID), PartID: idp(c.PartID), ReleaseID: idp(c.ReleaseID), OrgID: idp(c.OrgID)}
	if a := c.Edges.Alias; a != nil {
		out.Alias = &AliasRef{ID: id(a.ID), Name: a.Name, Latin: a.Latin, PersonID: id(a.PersonID)}
	}
	return out
}
