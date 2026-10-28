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
	JSON        bool
	Buffered    bool
	Extra       map[string]json.RawMessage
	Site        string
	UserID      int64
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
