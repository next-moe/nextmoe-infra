package commit

import "api/internal/canon/ent"

type builder struct {
	create func(c *ent.Client) ent.Mutation
	update func(c *ent.Client, id int64) ent.Mutation
}

// builders is every entity a changeset may write; Revision is absent on
// purpose, only the commit service itself writes it.
var builders = map[string]builder{
	"work": {func(c *ent.Client) ent.Mutation { return c.Work.Create().Mutation() },
		func(c *ent.Client, id int64) ent.Mutation { return c.Work.UpdateOneID(id).Mutation() }},
	"work_title": {func(c *ent.Client) ent.Mutation { return c.WorkTitle.Create().Mutation() },
		func(c *ent.Client, id int64) ent.Mutation { return c.WorkTitle.UpdateOneID(int(id)).Mutation() }},
	"work_part": {func(c *ent.Client) ent.Mutation { return c.WorkPart.Create().Mutation() },
		func(c *ent.Client, id int64) ent.Mutation { return c.WorkPart.UpdateOneID(id).Mutation() }},
	"release": {func(c *ent.Client) ent.Mutation { return c.Release.Create().Mutation() },
		func(c *ent.Client, id int64) ent.Mutation { return c.Release.UpdateOneID(id).Mutation() }},
	"release_content": {func(c *ent.Client) ent.Mutation { return c.ReleaseContent.Create().Mutation() },
		func(c *ent.Client, id int64) ent.Mutation { return c.ReleaseContent.UpdateOneID(int(id)).Mutation() }},
	"book_work": {func(c *ent.Client) ent.Mutation { return c.BookWork.Create().Mutation() },
		func(c *ent.Client, id int64) ent.Mutation { return c.BookWork.UpdateOneID(int(id)).Mutation() }},
	"book_release": {func(c *ent.Client) ent.Mutation { return c.BookRelease.Create().Mutation() },
		func(c *ent.Client, id int64) ent.Mutation { return c.BookRelease.UpdateOneID(int(id)).Mutation() }},
	"person": {func(c *ent.Client) ent.Mutation { return c.Person.Create().Mutation() },
		func(c *ent.Client, id int64) ent.Mutation { return c.Person.UpdateOneID(id).Mutation() }},
	"person_alias": {func(c *ent.Client) ent.Mutation { return c.PersonAlias.Create().Mutation() },
		func(c *ent.Client, id int64) ent.Mutation { return c.PersonAlias.UpdateOneID(id).Mutation() }},
	"org": {func(c *ent.Client) ent.Mutation { return c.Org.Create().Mutation() },
		func(c *ent.Client, id int64) ent.Mutation { return c.Org.UpdateOneID(id).Mutation() }},
	"org_name": {func(c *ent.Client) ent.Mutation { return c.OrgName.Create().Mutation() },
		func(c *ent.Client, id int64) ent.Mutation { return c.OrgName.UpdateOneID(int(id)).Mutation() }},
	"credit": {func(c *ent.Client) ent.Mutation { return c.Credit.Create().Mutation() },
		func(c *ent.Client, id int64) ent.Mutation { return c.Credit.UpdateOneID(id).Mutation() }},
}
