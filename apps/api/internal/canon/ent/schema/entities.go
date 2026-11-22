package schema

import "entgo.io/ent"

// Entities is every canon schema the ingest contract describes; a schema
// missing here is invisible to ingest (TestEveryEntSchemaIsListed).
var Entities = []ent.Interface{
	Work{}, WorkTitle{}, WorkPart{},
	Release{}, ReleaseContent{}, BookWork{}, BookRelease{},
	Person{}, PersonAlias{}, Org{}, OrgName{}, Credit{},
	Revision{}, EvidenceSnapshot{}, EvidenceRecord{}, CommitKey{}, FaninCursor{},
	Todo{}, Hold{}, Takedown{},
}
