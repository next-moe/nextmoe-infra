package editing

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrUnknownEntityType  = errors.New("editing: entity type not registered")
	ErrEntityNotFound     = errors.New("editing: entity not found")
	ErrProposalNotFound   = errors.New("editing: proposal not found")
	ErrRevisionNotFound   = errors.New("editing: revision not found")
	ErrEmptyPatch         = errors.New("editing: patch has no fields")
	ErrEmptyDelta         = errors.New("editing: amendment delta is empty")
	ErrNotOpen            = errors.New("editing: proposal is not open")
	ErrNotProposer        = errors.New("editing: only the proposer may withdraw")
	ErrNoEffectiveChanges = errors.New("editing: patch has no effective changes")
	ErrNoBaseline         = errors.New("editing: no state before this entity's first edit was recorded")
)

type UnknownFieldError struct{ Key string }

func (e *UnknownFieldError) Error() string {
	return fmt.Sprintf("editing: unknown field key %q", e.Key)
}

type LockedFieldError struct{ Key string }

func (e *LockedFieldError) Error() string {
	return fmt.Sprintf("editing: field %q is locked", e.Key)
}

type ValidationError struct {
	Key    string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("editing: field %q: %s", e.Key, e.Reason)
}

type PermissionError struct {
	Key    string
	Action string
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("editing: not allowed to %s field %q", e.Action, e.Key)
}

type ConflictError struct{ Keys []string }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("editing: conflicting fields: %s", strings.Join(e.Keys, ", "))
}

type UndoConflictError struct{ Keys []string }

func (e *UndoConflictError) Error() string {
	return fmt.Sprintf("editing: a later revision changed: %s", strings.Join(e.Keys, ", "))
}
