package handler

import (
	"context"
	"encoding/json"
	"strings"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/apiv2/vocab"
	catsvc "api/internal/platform/catalog/service"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"
)

func (c *Catalog) GetEditContext(ctx context.Context, object, rawID string) (repr.EditContext, error) {
	entityType := schemaEntityType(object)
	if entityType == "" {
		return repr.EditContext{}, problem.New(problem.CodeNotFound, "", "", "No editable family "+object+".")
	}
	id, ok := repr.ParseID(rawID)
	if !ok || id <= 0 {
		return repr.EditContext{}, problem.New(problem.CodeNotFound, "", "", "No entity with this id.")
	}
	if c == nil || c.EditTypes == nil || c.EditContext == nil {
		return repr.EditContext{}, problem.New(problem.CodeServiceUnavailable, "", "", "the editing engine is not bound.")
	}
	if _, err := c.policyActor(ctx); err != nil {
		return repr.EditContext{}, err
	}
	spec, ok := c.EditTypes.Type(entityType)
	if !ok {
		return repr.EditContext{}, problem.New(problem.CodeServiceUnavailable, "", "", "the editing engine is not bound.")
	}
	snapshot, err := spec.LoadSnapshot(ctx, id)
	if err != nil {
		return repr.EditContext{}, proposalErr(err)
	}
	sources, err := c.EditContext.SourceKeys(ctx)
	if err != nil {
		return repr.EditContext{}, err
	}
	display := sourceDisplayNames()
	heads := map[string]map[string][]provenance.Entry{}

	fields := make([]repr.EditContextField, 0, len(spec.Fields))
	for i := range spec.Fields {
		f := &spec.Fields[i]
		out := repr.EditContextField{Key: f.Key, Value: snapshot[f.Key], Provenance: []repr.ColumnProvenance{}}
		if f.Kind != editing.KindList {
			for _, t := range f.Provenance {
				if t.Rows != nil {
					continue
				}
				head, ok := heads[t.Table]
				if !ok {
					if head, err = c.EditContext.HeadProvenance(ctx, t.Table, id); err != nil {
						return repr.EditContext{}, err
					}
					heads[t.Table] = head
				}
				out.Provenance = append(out.Provenance, repr.ColumnProvenance{
					Column: t.Column, History: provenanceHistory(head[t.Column]),
				})
			}
		} else if !strings.HasSuffix(f.Key, editing.SuppressedFieldSuffix) {
			rows, truncated, err := c.EditContext.Rows(ctx, entityType, f.Key, id, sources)
			if err != nil {
				return repr.EditContext{}, err
			}
			if rows != nil {
				mapped := make([]repr.EditContextRow, 0, len(rows))
				for _, r := range rows {
					mapped = append(mapped, contextRowRepr(r, display))
				}
				out.Rows = &mapped
				out.RowsTruncated = truncated
			}
		}
		fields = append(fields, out)
	}
	return repr.EditContext{
		Object: "edit_context", TargetObject: object, EntityType: entityType,
		EntityID: repr.ID(id), Fields: fields,
	}, nil
}

func sourceDisplayNames() map[string]string {
	out := map[string]string{}
	if v, ok := vocab.Lookup("sources"); ok {
		for _, x := range v.Values {
			out[x.Value] = x.DisplayName
		}
	}
	return out
}

func provenanceHistory(entries []provenance.Entry) []repr.ProvenanceEntry {
	out := make([]repr.ProvenanceEntry, 0, len(entries))
	for _, e := range entries {
		pe := repr.ProvenanceEntry{Source: e.Source, Human: provenance.IsHuman(e.Source)}
		if e.At != "" {
			at := e.At
			pe.At = &at
		}
		out = append(out, pe)
	}
	return out
}

func contextRowRepr(r catsvc.ContextRow, display map[string]string) repr.EditContextRow {
	out := repr.EditContextRow{
		HumanLane: r.HumanLane, Suppressed: r.Suppressed, Present: r.Present,
		MachineTranslated: r.MachineTranslated,
	}
	if r.SourceKey != "" {
		name := display[r.SourceKey]
		if name == "" {
			name = r.SourceKey
		}
		out.Source = &repr.EditSource{Key: r.SourceKey, Display: name}
	}
	if r.Identity != "" {
		key := r.Identity
		out.Identity = &key
	}
	if len(r.Value) > 0 {
		var v any
		if json.Unmarshal(r.Value, &v) == nil {
			out.Value = v
		}
	}
	if len(r.FieldProvenance) > 0 {
		var m map[string]any
		if json.Unmarshal(r.FieldProvenance, &m) == nil && len(m) > 0 {
			out.FieldProvenance = m
		}
	}
	return out
}
