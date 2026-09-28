package wireshape

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

type child struct {
	Name string `json:"name"`
}

type sentBody struct {
	Tags []string `json:"tags"`
}

type answer struct {
	Items  []string `json:"items"`
	Child  *child   `json:"child"`
	Absent *child   `json:"absent,omitempty"`
	Level  *string  `json:"level" enum:"all,feed"`
}

func TestPublish(t *testing.T) {
	_, api := humatest.New(t)
	huma.Register(api, huma.Operation{OperationID: "echo", Method: http.MethodPost, Path: "/echo"},
		func(context.Context, *struct{ Body sentBody }) (*struct{ Body answer }, error) { return nil, nil })
	doc := api.OpenAPI()
	Publish(doc)
	schemas := doc.Components.Schemas.Map()

	if !schemas["SentBody"].Properties["tags"].Nullable {
		t.Error("a request list must still admit null")
	}
	got := schemas["Answer"].Properties
	if got["items"].Nullable {
		t.Error("a response list is never null")
	}
	if c := got["child"]; len(c.AnyOf) != 2 || c.AnyOf[0].Ref != "#/components/schemas/Child" || c.AnyOf[1].Type != "null" {
		t.Errorf("a nil pointer without omitempty is sent as null: %+v", c)
	}
	if got["absent"].Ref != "#/components/schemas/Child" {
		t.Errorf("an omitempty pointer is left out, never null: %+v", got["absent"])
	}
	if l := got["level"]; !l.Nullable || !slices.Contains(l.Enum, nil) {
		t.Errorf("a nullable enum lists null: %+v", l)
	}
}
