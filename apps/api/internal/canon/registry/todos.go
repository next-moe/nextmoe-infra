package registry

// TodoKinds is every kind fanin files a todo under. perm maps each to the key
// that decides it; a kind missing there could be listed but never settled.
const (
	TodoIdentity     = "identity"
	TodoAgentOnHuman = "agent_on_human"
	TodoFirstWrite   = "first_write"
	TodoConflict     = "conflict"
)

var TodoKinds = []string{TodoIdentity, TodoAgentOnHuman, TodoFirstWrite, TodoConflict}
