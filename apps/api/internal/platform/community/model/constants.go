package model

import "slices"

const (
	ThreadKindTopic    int16 = 0
	ThreadKindComments int16 = 1
	ThreadKindFeedback int16 = 2
)

const (
	AnchorKindBoard         int16 = 0
	AnchorKindSiteGame      int16 = 1
	AnchorKindSiteResource  int16 = 2
	AnchorKindCatalogWork   int16 = 3
	AnchorKindCatalogPerson int16 = 4
)

// SiteLocalAnchorKinds carry a tenant-local id: two sites can mint the same one,
// so identity includes the site. Every other anchor kind is a network-global id
// whose conversation is shared across sites (invariant 1). SQL that scopes a
// read to a caller must mirror this list, not restate it.
var SiteLocalAnchorKinds = []int16{AnchorKindBoard, AnchorKindSiteGame, AnchorKindSiteResource}

func AnchorIsSiteLocal(anchorKind int16) bool {
	return slices.Contains(SiteLocalAnchorKinds, anchorKind)
}

const (
	ContentRatingAll int16 = 0
	ContentRatingR15 int16 = 1
	ContentRatingR18 int16 = 2
)

const (
	ThreadStatusOpen    int16 = 0
	ThreadStatusClosed  int16 = 1
	ThreadStatusHidden  int16 = 2
	ThreadStatusDeleted int16 = 3
)

const (
	FeedbackStatusOpen      int16 = 0
	FeedbackStatusConfirmed int16 = 1
	FeedbackStatusPlanned   int16 = 2
	FeedbackStatusFixed     int16 = 3
	FeedbackStatusDeclined  int16 = 4
	FeedbackStatusDuplicate int16 = 5
)

const (
	PostStatusVisible int16 = 0
	PostStatusHidden  int16 = 1
	PostStatusDeleted int16 = 2
)

const (
	ReactionKindLike int16 = 0
)

const (
	NotificationLevelMuted    int16 = 0
	NotificationLevelNormal   int16 = 1
	NotificationLevelTracking int16 = 2
	NotificationLevelWatching int16 = 3
)

const (
	BoardFormatDiscussion   int16 = 0
	BoardFormatQA           int16 = 1
	BoardFormatAnnouncement int16 = 2
)

const (
	TrustLevelNew     int16 = 0
	TrustLevelBasic   int16 = 1
	TrustLevelMember  int16 = 2
	TrustLevelRegular int16 = 3
	TrustLevelLeader  int16 = 4
)

const (
	GrantedBoostNone    int16 = 0
	GrantedBoostVeteran int16 = 1
	GrantedBoostCreator int16 = 2
	GrantedBoostStaff   int16 = 3
)

const (
	FlagReasonSpam         int16 = 0
	FlagReasonAbuse        int16 = 1
	FlagReasonOffTopic     int16 = 2
	FlagReasonOther        int16 = 3
	FlagReasonNsfwMislabel int16 = 4
)

const (
	FlagStatusPending   int16 = 0
	FlagStatusAgreed    int16 = 1
	FlagStatusDisagreed int16 = 2
	FlagStatusIgnored   int16 = 3
)

const (
	ReviewSourceFlags         int16 = 0
	ReviewSourceFirstPostHold int16 = 1
	ReviewSourceSuspectWords  int16 = 2
	ReviewSourceExternal      int16 = 3
)

const (
	ReviewStatusPending  int16 = 0
	ReviewStatusApproved int16 = 1
	ReviewStatusRejected int16 = 2
)
