package handler

import (
	"api/internal/platform/apiv2/parse"
	"api/internal/platform/apiv2/problem"
)

type traitFilter struct {
	ParentID int64
	GroupID  int64
	Root     *bool
}

func parseTraitFilter(in *listTraitsInput) (traitFilter, *problem.Problem) {
	if in == nil {
		return traitFilter{}, nil
	}
	parentID, err := optionalID(in.ParentID, "parent_id")
	if err != nil {
		return traitFilter{}, err
	}
	groupID, err := optionalID(in.GroupID, "group_id")
	if err != nil {
		return traitFilter{}, err
	}
	var root *bool
	if in.Root != "" {
		v, berr := parse.Bool(in.Root, "root")
		if berr != nil {
			return traitFilter{}, berr
		}
		root = &v
	}
	return traitFilter{ParentID: parentID, GroupID: groupID, Root: root}, nil
}
