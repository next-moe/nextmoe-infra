package service

import "errors"

var (
	ErrThreadNotFound  = errors.New("community: thread not found")
	ErrThreadNotOpen   = errors.New("community: thread not open")
	ErrNotFeedback     = errors.New("community: thread is not a feedback thread")
	ErrPostNotFound    = errors.New("community: post not found")
	ErrReviewNotFound  = errors.New("community: review item not found")
	ErrNotAuthor       = errors.New("community: not the post author")
	ErrPostNotEditable = errors.New("community: post not editable")
	ErrContentBlocked  = errors.New("community: content blocked by word list")

	ErrInvalidNotificationLevel = errors.New("community: notification level out of range")
	ErrInvalidSearchQuery       = errors.New("community: search query must be 2-100 characters")
)

type SandboxError struct{ Reason string }

func (e *SandboxError) Error() string { return "community: sandbox limit: " + e.Reason }
