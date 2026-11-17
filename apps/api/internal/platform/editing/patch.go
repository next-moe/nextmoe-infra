package editing

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"gorm.io/datatypes"
)

type Delta struct {
	Set   map[string]any `json:"set,omitempty"`
	Unset []string       `json:"unset,omitempty"`
}

func decodeDelta(raw datatypes.JSON) (Delta, error) {
	var d Delta
	if err := json.Unmarshal(raw, &d); err != nil {
		return Delta{}, fmt.Errorf("editing: decode delta: %w", err)
	}
	return d, nil
}

func decodeObject(raw datatypes.JSON) (map[string]any, error) {
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("editing: decode object: %w", err)
	}
	return m, nil
}

func decodeKeys(raw datatypes.JSON) ([]string, error) {
	var keys []string
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, fmt.Errorf("editing: decode keys: %w", err)
	}
	return keys, nil
}

func encodeJSON(v any) (datatypes.JSON, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("editing: encode json: %w", err)
	}
	return datatypes.JSON(b), nil
}

func encodePrior(before map[string]any, changed []string) (datatypes.JSON, error) {
	prior := make(map[string]any, len(changed))
	for _, key := range changed {
		prior[key] = before[key]
	}
	return encodeJSON(prior)
}

func effectivePatch(p *Proposal, amendments []ProposalAmendment) (map[string]any, map[string]struct{}, error) {
	patch, err := decodeObject(p.Patch)
	if err != nil {
		return nil, nil, err
	}
	touched := make(map[string]struct{})
	for i := range amendments {
		d, err := decodeDelta(amendments[i].PatchDelta)
		if err != nil {
			return nil, nil, err
		}
		for k, v := range d.Set {
			patch[k] = v
			touched[k] = struct{}{}
		}
		for _, k := range d.Unset {
			delete(patch, k)
			touched[k] = struct{}{}
		}
	}
	return patch, touched, nil
}

func jsonValueEqual(a, b any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
