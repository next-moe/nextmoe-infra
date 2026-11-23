package registry

// TodoKinds is every kind fanin files a todo under, the part of its subject
// key before the first colon. perm maps each to the key that decides it; a
// kind missing there could be listed but never settled.
const (
	TodoIdentity     = "identity"
	TodoAgentOnHuman = "agent_on_human"
)

var TodoKinds = []string{TodoIdentity, TodoAgentOnHuman}
