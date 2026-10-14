package editing

const (
	StatusOpen      int16 = 0
	StatusMerged    int16 = 1
	StatusDeclined  int16 = 2
	StatusWithdrawn int16 = 3
)

var StatusName = map[int16]string{
	StatusOpen:      "open",
	StatusMerged:    "merged",
	StatusDeclined:  "declined",
	StatusWithdrawn: "withdrawn",
}

const (
	ActionCreated  int16 = 0
	ActionMerged   int16 = 1
	ActionDirect   int16 = 2
	ActionReverted int16 = 3
)

var ActionName = map[int16]string{
	ActionCreated:  "created",
	ActionMerged:   "merged",
	ActionDirect:   "direct",
	ActionReverted: "reverted",
}

const TrustedTier int16 = 2

type PolicyContext struct {
	UserID           int64
	Site             string
	TrustTier        int16
	IsEntityOwner    bool
	ModerationCapped bool
	HasPerm          func(key string) bool
}

func (pc PolicyContext) hasPerm(key string) bool {
	return pc.HasPerm != nil && pc.HasPerm(key)
}
