package editorigin

import (
	"context"
	"testing"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		name       string
		client     string
		mayDeclare bool
		declared   string
		want       string
	}{
		{"site client keeps its own origin", Kungal, false, "", Kungal},
		{"site client cannot claim another site", Kungal, false, Letmoe, Kungal},
		{"editor takes the declared site", Editor, true, Moyu, Moyu},
		{"editor ignores a value off the list", Editor, true, "galgame_wiki", Editor},
		{"editor without a declaration is itself", Editor, true, "", Editor},
		{"non-edit client stays empty", "", false, Kungal, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Resolve(c.client, c.mayDeclare, c.declared); got != c.want {
				t.Fatalf("Resolve() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestContextRoundTrip(t *testing.T) {
	if Ptr(context.Background()) != nil {
		t.Fatal("no origin must write NULL")
	}
	if From(With(context.Background(), "spoofed")) != "" {
		t.Fatal("an off-list value must not come back out")
	}
	got := Ptr(With(context.Background(), Letmoe))
	if got == nil || *got != Letmoe {
		t.Fatalf("Ptr() = %v", got)
	}
}
