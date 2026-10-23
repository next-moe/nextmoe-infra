package service

const (
	EventPostCreated           = "post.created"
	EventPostEdited            = "post.edited"
	EventReplyToYou            = "reply.to_you"
	EventMention               = "mention"
	EventFeedbackStatusChanged = "feedback.status_changed"
	EventFlagThreshold         = "flag.threshold"
	EventReviewEnqueued        = "review.enqueued"
	EventReviewApproved        = "review.approved"
	EventReviewRejected        = "review.rejected"
)

type Event struct {
	Kind         string
	ThreadID     int64
	PostID       int64
	ActorID      int64
	TargetID     int64
	ReviewItemID int64
	CheckSkipped bool
}

type EventSink interface{ Emit(Event) }

type NoopSink struct{}

func (NoopSink) Emit(Event) {}
