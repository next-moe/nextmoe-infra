// Package registry is the single declaration of every writable canon property
// (plan 03 §5). It rides on the ent schema as a field annotation; validation,
// unique indexes, the ingest contract and the editor schema are derived from it.
package registry

type Shape string

const (
	ShapeText        Shape = "text"
	ShapeTitle       Shape = "title"
	ShapePersonName  Shape = "person_name"
	ShapePartialDate Shape = "partial_date"
	ShapeISBN13      Shape = "isbn13"
	ShapeJAN         Shape = "jan"
	ShapeMoney       Shape = "money"
	ShapeInteger     Shape = "integer"
	ShapeOrdinal     Shape = "ordinal"
	ShapeVocab       Shape = "vocab"
	ShapeLang        Shape = "lang"
	ShapeURL         Shape = "url"
	ShapeRef         Shape = "ref"
	ShapeCurrency    Shape = "currency"
	ShapeBool        Shape = "bool"
)

type Tier string

const (
	TierOfficial  Tier = "official"
	TierRegistry  Tier = "registry"
	TierRetailer  Tier = "retailer"
	TierCommunity Tier = "community"
	TierLegacy    Tier = "legacy"
	TierMachine   Tier = "machine"
)

var tierOrder = []Tier{TierOfficial, TierRegistry, TierRetailer, TierCommunity, TierLegacy, TierMachine}

// Rank orders tiers, 0 strongest; an unknown tier ranks last.
func Rank(t Tier) int {
	for i, x := range tierOrder {
		if x == t {
			return i
		}
	}
	return len(tierOrder)
}

type Unique string

const (
	UniqueNone   Unique = ""
	UniqueGlobal Unique = "global"
)

const AnnotationName = "CanonProperty"

type Property struct {
	Shape   Shape    `json:"shape"`
	Vocab   string   `json:"vocab,omitempty"`
	Ref     string   `json:"ref,omitempty"`
	Tiers   []Tier   `json:"tiers,omitempty"`
	Unique  Unique   `json:"unique,omitempty"`
	Agent   bool     `json:"agent"`
	Clauses []string `json:"clauses,omitempty"`
	// ScopedByProgram: the value is the writing program's medium, not a fact
	// read off a page, so it carries no evidence.
	ScopedByProgram bool   `json:"scoped_by_program,omitempty"`
	Doc             string `json:"doc"`
}

func (Property) Name() string { return AnnotationName }

var FactTiers = []Tier{TierOfficial, TierRegistry, TierRetailer}

func Fact(shape Shape, doc string, clauses ...string) Property {
	return Property{Shape: shape, Tiers: FactTiers, Agent: true, Clauses: clauses, Doc: doc}
}

func VocabFact(vocab, doc string, clauses ...string) Property {
	p := Fact(ShapeVocab, doc, clauses...)
	p.Vocab = vocab
	return p
}

func ProgramScoped(vocab, doc string, clauses ...string) Property {
	p := VocabFact(vocab, doc, clauses...)
	p.ScopedByProgram = true
	return p
}

func RefFact(entity, doc string, clauses ...string) Property {
	p := Fact(ShapeRef, doc, clauses...)
	p.Ref = entity
	return p
}

func Internal(shape Shape, doc string) Property {
	return Property{Shape: shape, Doc: doc}
}
