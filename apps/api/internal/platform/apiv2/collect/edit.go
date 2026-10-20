package collect

var RevisionSort = []string{"recorded_desc", "recorded_asc"}

var RevisionBasicFields = []string{
	"object", "id", "target_object", "entity_id", "site_work_id", "seq", "action",
	"changed_fields", "actor_uid", "amender_uid", "proposal_id", "site", "created_at",
	"has_baseline",
}

func RevisionSpec() Spec {
	fields := append([]string{}, RevisionBasicFields...)
	fields = append(fields, "diff", "diff_base", "diff_from_baseline")
	return Spec{
		Sort:    RevisionSort,
		Include: []string{"diff"},
		FullSet: []string{"diff"},
		Fields:  fields,
	}
}

func ClaimEventSpec() Spec {
	return Spec{
		Sort:    RevisionSort,
		Include: []string{},
		FullSet: []string{},
		Fields: []string{
			"object", "id", "work_id", "from_state", "to_state", "reason",
			"actor_uid", "site", "product_work_id", "created_at",
		},
	}
}

var ProposalBasicFields = []string{
	"object", "id", "state", "target_object", "entity_type", "entity_id", "note",
	"proposer_uid", "site", "base_revision_seq", "decided_by_uid", "decided_at",
	"created_at", "updated_at",
}

// The public face publishes no patch: an open proposal's raw payload is not one
// of the transparency reads, and the merged outcome is already addressable as a
// revision diff.
func PublicProposalSpec() Spec {
	fields := append([]string{}, ProposalBasicFields...)
	fields = append(fields, "amendments")
	return Spec{
		Sort:    []string{"filed_desc"},
		Include: []string{"amendments"},
		FullSet: []string{"amendments"},
		Fields:  fields,
	}
}

// ProposalListSpec is the moderation LIST lane, which parsed with ClaimSpec:
// fields=note and fields=entity_id were 400 UNKNOWN_FIELD on a face whose items
// are proposals, while fields=acted_count and fields=product_work_id were
// accepted and then projected away to nothing. ProposalSpec is the detail spec
// and is not NoBatch, which would open a hydration lane this list does not have.
var ProposalQueueSort = []string{"filed_desc", "filed_asc", "updated_desc", "updated_asc"}

var ProposalPatchFields = []string{"patch", "effective_patch", "changed_fields", "conflict_fields"}

func ProposalListSpec() Spec {
	fields := append([]string{}, ProposalBasicFields...)
	fields = append(fields, ProposalPatchFields...)
	return Spec{
		Sort:    ProposalQueueSort,
		Include: []string{"patch"},
		FullSet: []string{"patch"},
		Fields:  fields,
		NoBatch: true,
	}
}

func MyProposalListSpec() Spec {
	fields := append([]string{}, ProposalBasicFields...)
	fields = append(fields, ProposalPatchFields...)
	return Spec{
		Sort:    ProposalQueueSort,
		Include: []string{"patch"},
		FullSet: []string{"patch"},
		Fields:  fields,
		NoBatch: true,
	}
}

func ProposalSpec() Spec {
	fields := append([]string{}, ProposalBasicFields...)
	fields = append(fields, "amendments")
	fields = append(fields, ProposalPatchFields...)
	return Spec{
		Sort:    []string{"filed_desc"},
		Include: []string{"amendments", "patch"},
		FullSet: []string{"amendments", "patch"},
		Fields:  fields,
	}
}
