package llm

import "encoding/json"

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Message struct {
	Role       Role
	Content    string
	Raw        json.RawMessage
	ToolCalls  []ToolCall
	ToolCallID string
}

type Tool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

const (
	ToolChoiceAuto = "auto"
	ToolChoiceNone = "none"
)

type Call struct {
	Scene       string
	Model       string
	AffinityKey string
	Messages    []Message
	Tools       []Tool
	ToolChoice  string
	MaxTokens   int
	Reasoning   string
	JSON        bool
	Buffered    bool
	Extra       map[string]json.RawMessage
	Site        string
	UserID      int64
	AccessKeyID int64
	probe       bool
}

type EventKind int

const (
	EventText EventKind = iota
	EventReasoning
	EventToolCall
)

type Event struct {
	Kind     EventKind
	Text     string
	ToolCall ToolCall
}

type Usage struct {
	InputTokens     int
	CachedTokens    int
	OutputTokens    int
	ReasoningTokens int
	Estimated       bool
	Units           int
	ReportedNano    int64
}

// reportedNano reads the cost an upstream states in its usage block. xAI gives
// cost_in_usd_ticks, 1e10 ticks to the dollar (a 0.02 USD picture came back as
// 2e8), and sometimes cost_in_nano_usd as well.
func reportedNano(ticks, nano int64) int64 {
	if nano > 0 {
		return nano
	}
	return ticks / 10
}

type Result struct {
	Text         string
	Reasoning    string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        Usage
	Model        string
	CallID       string
}
