package walkthrough

const ChartVersion = 1

const (
	NodeEntry   = "entry"
	NodeSave    = "save"
	NodeEnding  = "ending"
	NodeCollect = "collect"
	NodeOpen    = "open"
)

const (
	StepChoice  = "choice"
	StepAction  = "action"
	StepSave    = "save"
	StepHeading = "heading"
	StepNote    = "note"
)

var EndingTypes = []string{"heroine", "harem", "true", "normal", "bad", "after", "other"}

type Chart struct {
	Characters []Character `json:"characters"`
	Sections   []Section   `json:"sections"`
	Entries    []Entry     `json:"entries"`
	Saves      []Save      `json:"saves"`
	Endings    []Ending    `json:"endings"`
	Segments   []Segment   `json:"segments"`
}

type Character struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

type Section struct {
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	TitleZh    *string  `json:"title_zh"`
	Characters []string `json:"characters"`
}

type Entry struct {
	Key        string  `json:"key"`
	Label      string  `json:"label"`
	LabelZh    *string `json:"label_zh"`
	UnlockedBy *string `json:"unlocked_by"`
}

type Save struct {
	Key     string  `json:"key"`
	Label   string  `json:"label"`
	LabelZh *string `json:"label_zh"`
}

type Ending struct {
	Key        string   `json:"key"`
	Name       string   `json:"name"`
	NameZh     *string  `json:"name_zh"`
	EndingType string   `json:"ending_type"`
	Characters []string `json:"characters"`
}

type Segment struct {
	Key             string   `json:"key"`
	Section         *string  `json:"section"`
	From            Node     `json:"from"`
	Condition       *string  `json:"condition"`
	ConditionZh     *string  `json:"condition_zh"`
	RequiresEndings []string `json:"requires_endings"`
	Steps           []Step   `json:"steps"`
	To              Node     `json:"to"`
}

type Node struct {
	NodeType string  `json:"node_type"`
	Key      *string `json:"key"`
}

type Step struct {
	StepType    string    `json:"step_type"`
	Text        *string   `json:"text"`
	TextZh      *string   `json:"text_zh"`
	Alt         []string  `json:"alt"`
	AltZh       []*string `json:"alt_zh"`
	Achievement bool      `json:"achievement"`
	Save        *string   `json:"save"`
}
