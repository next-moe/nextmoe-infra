package service

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"api/internal/platform/auth/model"
	"api/internal/platform/permissions"
	"api/internal/platform/settings/keys"
	"api/pkg/errors"
	"api/pkg/trustclient"

	"golang.org/x/text/unicode/norm"
)

const (
	ProfileSubjectKind  = "user_profile"
	profileCheckTimeout = 500 * time.Millisecond
	profileScanTimeout  = 10 * time.Second
)

type ProfileTrust interface {
	Check(ctx context.Context, req trustclient.CheckRequest) (decision string, matched []string, err error)
	Scan(ctx context.Context, req trustclient.ScanRequest) (scanID int64, truncated bool, err error)
}

// Contact details alone are not refused: the 2026-10-31 review of every
// profile set the 980 accounts that only left a QQ group or a site address
// aside as acceptable. What it banned on sight was solicitation carrying a
// handle — 外約…TG：xs9805, 學生妹 + 賴 — so refusing needs both halves.
var (
	solicitationRE = regexp.MustCompile(`外约|外約|外送|约炮|約炮|喝茶|品茶|茶庄|茶莊|上门|上門|包夜|援交|楼凤|樓鳳|找小姐|学生妹|學生妹|兼职妹|兼職妹|约妹|約妹|裸聊|迷药|迷藥|博彩|娱乐城|娛樂城|百家乐|百家樂|代孕`)
	contactRE      = regexp.MustCompile(`(?i)(?:tg|telegram|电报|電報|飞机|飛機|line|赖|賴|gleezy|vx|wx|v信|微信|威信|薇信|whatsapp|qq|扣扣|skype|kakao)[:@]*(?:搜索?|号|號|id)?[:@]*[a-z0-9_\-]{4,}|@[a-z0-9_]{5,}|1[3-9]\d{9}`)
)

func compactProfileText(s string) string {
	s = norm.NFKC.String(s)
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

func solicitsWithContact(s string) bool {
	c := compactProfileText(s)
	return solicitationRE.MatchString(c) && contactRE.MatchString(c)
}

func isOrdinary(roles []string) bool {
	for _, r := range roles {
		if r != permissions.RoleUser {
			return false
		}
	}
	return true
}

func profileText(fields ...string) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			parts = append(parts, f)
		}
	}
	return strings.Join(parts, "\n\n")
}

func labelledProfile(name, bio, about string) string {
	lines := []string{"用户名: " + name}
	if bio = strings.TrimSpace(bio); bio != "" {
		lines = append(lines, "签名: "+bio)
	}
	if about = strings.TrimSpace(about); about != "" {
		lines = append(lines, "about: "+about)
	}
	return strings.Join(lines, "\n")
}

type ProfileReview struct {
	trust ProfileTrust
}

func NewProfileReview(trust ProfileTrust) *ProfileReview {
	return &ProfileReview{trust: trust}
}

// Gate refuses what an ordinary user is about to write into their name, bio
// or about. Only the fields being changed are passed in, so an old profile
// never blocks an unrelated edit. Trust is consulted for banned words only and
// fails open, like every other caller of /trust/check.
func (p *ProfileReview) Gate(ctx context.Context, userID uint, roles []string, texts ...string) error {
	if p == nil || !keys.AuthProfileCheckEnabled.Get() || !isOrdinary(roles) {
		return nil
	}
	text := profileText(texts...)
	if text == "" {
		return nil
	}
	if solicitsWithContact(text) {
		return errors.NewWithCode(errors.ErrAuthProfileRejected)
	}
	if p.trust == nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, profileCheckTimeout)
	defer cancel()
	req := trustclient.CheckRequest{Text: text}
	if userID != 0 {
		author := int64(userID)
		req.AuthorID = &author
	}
	decision, _, err := p.trust.Check(cctx, req)
	if err != nil {
		slog.Warn("profile trust-check fail-open", "user_id", userID, "err", err)
		return nil
	}
	if decision == "deny" {
		return errors.NewWithCode(errors.ErrAuthProfileRejected)
	}
	return nil
}

// Submit sends an ordinary user's whole current profile to Trust for an
// asynchronous verdict, off the request.
func (p *ProfileReview) Submit(u *model.User) {
	if p == nil || p.trust == nil || u == nil || !keys.AuthProfileScanEnabled.Get() || !isOrdinary(u.RoleNames()) {
		return
	}
	text := labelledProfile(u.Name, u.Bio, u.About)
	author := int64(u.ID)
	req := trustclient.ScanRequest{
		SubjectKind: ProfileSubjectKind,
		SubjectID:   strconv.FormatUint(uint64(u.ID), 10),
		Text:        text,
		AuthorID:    &author,
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), profileScanTimeout)
		defer cancel()
		if _, _, err := p.trust.Scan(ctx, req); err != nil {
			slog.Warn("profile trust-scan failed", "user_id", req.SubjectID, "err", err)
		}
	}()
}
