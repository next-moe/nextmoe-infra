package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
	"api/internal/platform/community/service"
	siteModel "api/internal/platform/site/model"
	"api/pkg/errors"

	"github.com/danielgtaylor/huma/v2"
)

const activitySitesMax = 10

var siteNamePattern = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

func (s *Server) registerActivities(api huma.API) {
	tags := []string{"community-activities"}
	huma.Register(api, huma.Operation{OperationID: "writeActivities", Method: http.MethodPost, Path: "/api/v1/community/activities",
		Summary: "Push up to 100 of this site's activities (upserts and tombstones under the revision rule; one outcome per item)", Tags: tags}, s.writeActivities)
	huma.Register(api, huma.Operation{OperationID: "listSiteActivities", Method: http.MethodGet, Path: "/api/v1/community/activities",
		Summary: "This site's stored activities in id order, tombstones included, every stored field: the reconciliation read", Tags: tags}, s.listSiteActivities)
	huma.Register(api, huma.Operation{OperationID: "listFollowingActivities", Method: http.MethodGet, Path: "/api/v1/community/users/{id}/following/activities",
		Summary: "The following feed: activity groups of everyone the user follows, across every site, newest first", Tags: tags}, s.listFollowingActivities)
	huma.Register(api, huma.Operation{OperationID: "getFollowingActivitiesUnseen", Method: http.MethodGet, Path: "/api/v1/community/users/{id}/following/activities/unseen",
		Summary: "How many feed groups moved since the user last looked (the red dot)", Tags: tags}, s.getFollowingActivitiesUnseen)
	huma.Register(api, huma.Operation{OperationID: "markFollowingActivitiesSeen", Method: http.MethodPost, Path: "/api/v1/community/users/{id}/following/activities/seen",
		Summary: "Move the user's account-wide feed mark forward (never back, never past now)", Tags: tags}, s.markFollowingActivitiesSeen)
	huma.Register(api, huma.Operation{OperationID: "listUserActivities", Method: http.MethodGet, Path: "/api/v1/community/users/{id}/activities",
		Summary: "One user's own activity groups across every site, newest first", Tags: tags}, s.listUserActivities)
	huma.Register(api, huma.Operation{OperationID: "listActivityGroupItems", Method: http.MethodGet, Path: "/api/v1/community/activity-groups/{id}/items",
		Summary: "Every live item of one activity group, newest first", Tags: tags}, s.listActivityGroupItems)
}

type writeActivitiesInput struct{ Body dto.ActivityWriteRequest }
type writeActivitiesOutput struct {
	Body Envelope[dto.ActivityWriteResponse]
}

func (s *Server) writeActivities(ctx context.Context, in *writeActivitiesInput) (*writeActivitiesOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	items := make([]service.ActivityInput, len(in.Body.Items))
	for i, it := range in.Body.Items {
		items[i] = service.ActivityInput{
			Key: it.Key, ActorID: it.ActorID, Verb: it.Verb, ObjectKind: it.ObjectKind,
			ObjectLabel: it.ObjectLabel, Title: it.Title, Excerpt: it.Excerpt, URL: it.URL,
			CoverImageHash: it.CoverImageHash, WorkID: it.WorkID, ContentLimit: it.ContentLimit,
			Notify: it.Notify, OccurredAt: it.OccurredAt, Revision: it.Revision, Removed: it.Removed,
		}
	}
	results, err := s.activities.Write(ctx, site, activityURLHosts(clientFromCtx(ctx)), items)
	if err != nil {
		return nil, mapErr("write activities", err)
	}
	out := make([]dto.ActivityWriteOutcome, len(results))
	for i, r := range results {
		out[i] = dto.ActivityWriteOutcome{Key: r.Key, Outcome: r.Outcome, Reason: r.Reason}
	}
	return &writeActivitiesOutput{Body: okEnvelope(dto.ActivityWriteResponse{Results: out})}, nil
}

func activityURLHosts(client *siteModel.OAuthClient) []string {
	var uris []string
	if client == nil || json.Unmarshal(client.RedirectURIs, &uris) != nil {
		return nil
	}
	var hosts []string
	for _, raw := range uris {
		u, err := url.Parse(raw)
		if err == nil && u.Scheme == "https" && u.Hostname() != "" {
			hosts = append(hosts, strings.ToLower(u.Hostname()))
		}
	}
	return hosts
}

type listSiteActivitiesInput struct {
	Cursor string `query:"cursor" doc:"opaque cursor from the previous page"`
	Limit  int    `query:"limit" doc:"page size (max 1000, default 1000)"`
}
type listSiteActivitiesOutput struct {
	Body Envelope[dto.SiteActivityListResponse]
}

func (s *Server) listSiteActivities(ctx context.Context, in *listSiteActivitiesInput) (*listSiteActivitiesOutput, error) {
	site, he := siteBinding(ctx)
	if he != nil {
		return nil, he
	}
	var after int64
	if in.Cursor != "" {
		id, err := strconv.ParseInt(in.Cursor, 10, 64)
		if err != nil || id <= 0 {
			return nil, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "malformed cursor")
		}
		after = id
	}
	rows, limit, err := s.activities.ListOwn(site, after, in.Limit)
	if err != nil {
		return nil, mapErr("list site activities", err)
	}
	views := make([]dto.SiteActivityView, len(rows))
	for i := range rows {
		a := &rows[i]
		views[i] = dto.SiteActivityView{
			ID: a.ID, Key: a.Key, ActorID: a.ActorID, Verb: verbName(a.Verb), ObjectKind: a.ObjectKind,
			ObjectLabel: a.ObjectLabel, Title: a.Title, Excerpt: a.Excerpt, URL: a.URL,
			CoverImageHash: a.CoverImageHash, WorkID: a.WorkID, ContentLimit: contentLimitName(a.ContentLimit),
			Notify: a.Notify, OccurredAt: a.OccurredAt, Revision: a.Revision, Removed: a.RemovedAt != nil,
			CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt, RemovedAt: a.RemovedAt,
		}
	}
	next := ""
	if len(rows) == limit {
		next = strconv.FormatInt(rows[len(rows)-1].ID, 10)
	}
	return &listSiteActivitiesOutput{Body: okEnvelope(dto.SiteActivityListResponse{Activities: views, NextCursor: next})}, nil
}

type activityFeedInput struct {
	ID           int64  `path:"id" minimum:"1"`
	Cursor       string `query:"cursor" doc:"opaque cursor from the previous page"`
	Limit        int    `query:"limit" doc:"page size (max 50, default 20)"`
	ContentLimit string `query:"content_limit" default:"all" enum:"all,sfw" doc:"sfw = only SFW items, filtered here so a page is never thinned by the caller"`
	Sites        string `query:"sites" doc:"comma-separated sites to keep, e.g. kungal; empty = every site"`
	Verbs        string `query:"verbs" doc:"comma-separated verbs to keep (publish,reply,comment,rate,like,edit); empty = all"`
}
type activityGroupsOutput struct {
	Body Envelope[dto.ActivityGroupListResponse]
}

func (s *Server) listFollowingActivities(ctx context.Context, in *activityFeedInput) (*activityGroupsOutput, error) {
	return s.activityGroups(ctx, in, s.activities.FollowingFeed)
}

func (s *Server) listUserActivities(ctx context.Context, in *activityFeedInput) (*activityGroupsOutput, error) {
	return s.activityGroups(ctx, in, s.activities.ActorFeed)
}

func (s *Server) activityGroups(ctx context.Context, in *activityFeedInput,
	read func(service.ActivityFeedParams) ([]service.ActivityGroup, int, error)) (*activityGroupsOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	p, he := feedParams(in.ID, in.ContentLimit, in.Sites, in.Verbs, in.Cursor, in.Limit)
	if he != nil {
		return nil, he
	}
	groups, limit, err := read(p)
	if err != nil {
		return nil, mapErr("list activity groups", err)
	}
	views := make([]dto.ActivityGroupView, len(groups))
	for i := range groups {
		g := &groups[i]
		views[i] = dto.ActivityGroupView{
			ID: g.ID, Site: g.Site, ActorID: g.ActorID, Verb: verbName(g.Verb), ObjectKind: g.ObjectKind,
			ObjectLabel: g.ObjectLabel, Day: g.BucketDate, ItemCount: g.ItemCount, LatestAt: g.LatestAt,
			Items: toActivityItemViews(g.Items),
		}
	}
	next := ""
	if len(groups) == limit {
		last := groups[len(groups)-1]
		next = encodeFeedCursor(last.LatestAt, last.ID)
	}
	return &activityGroupsOutput{Body: okEnvelope(dto.ActivityGroupListResponse{Groups: views, NextCursor: next})}, nil
}

type activityGroupItemsInput struct {
	ID           int64  `path:"id" minimum:"1"`
	Cursor       string `query:"cursor" doc:"opaque cursor from the previous page"`
	Limit        int    `query:"limit" doc:"page size (max 50, default 20)"`
	ContentLimit string `query:"content_limit" default:"all" enum:"all,sfw"`
}
type activityItemsOutput struct {
	Body Envelope[dto.ActivityItemListResponse]
}

func (s *Server) listActivityGroupItems(ctx context.Context, in *activityGroupItemsInput) (*activityItemsOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	before, he := decodeFeedCursor(in.Cursor)
	if he != nil {
		return nil, he
	}
	items, limit, err := s.activities.GroupItems(in.ID, in.ContentLimit == "sfw", before, in.Limit)
	if err != nil {
		return nil, mapErr("list activity group items", err)
	}
	next := ""
	if len(items) == limit {
		last := items[len(items)-1]
		next = encodeFeedCursor(last.OccurredAt, last.ID)
	}
	return &activityItemsOutput{Body: okEnvelope(dto.ActivityItemListResponse{Items: toActivityItemViews(items), NextCursor: next})}, nil
}

type activityUnseenInput struct {
	ID           int64  `path:"id" minimum:"1"`
	ContentLimit string `query:"content_limit" default:"all" enum:"all,sfw"`
	Sites        string `query:"sites" doc:"comma-separated sites to keep; empty = every site"`
	Verbs        string `query:"verbs" doc:"comma-separated verbs to keep; empty = all"`
}
type activityUnseenOutput struct {
	Body Envelope[dto.ActivityUnseenResponse]
}

func (s *Server) getFollowingActivitiesUnseen(ctx context.Context, in *activityUnseenInput) (*activityUnseenOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	p, he := feedParams(in.ID, in.ContentLimit, in.Sites, in.Verbs, "", 0)
	if he != nil {
		return nil, he
	}
	n, seen, err := s.activities.Unseen(p)
	if err != nil {
		return nil, mapErr("count unseen activities", err)
	}
	return &activityUnseenOutput{Body: okEnvelope(dto.ActivityUnseenResponse{UnseenCount: n, SeenAt: seen})}, nil
}

type activitySeenInput struct {
	ID   int64 `path:"id" minimum:"1"`
	Body dto.ActivitySeenRequest
}
type activitySeenOutput struct {
	Body Envelope[dto.ActivitySeenResponse]
}

func (s *Server) markFollowingActivitiesSeen(ctx context.Context, in *activitySeenInput) (*activitySeenOutput, error) {
	if _, he := siteBinding(ctx); he != nil {
		return nil, he
	}
	seen, err := s.activities.MarkSeen(in.ID, in.Body.At)
	if err != nil {
		return nil, mapErr("mark activities seen", err)
	}
	return &activitySeenOutput{Body: okEnvelope(dto.ActivitySeenResponse{SeenAt: seen})}, nil
}

func feedParams(userID int64, contentLimit, sites, verbs, cursor string, limit int) (service.ActivityFeedParams, *houseError) {
	p := service.ActivityFeedParams{UserID: userID, SFW: contentLimit == "sfw", Limit: limit}
	for _, site := range splitList(sites) {
		if !siteNamePattern.MatchString(site) {
			return p, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "malformed site in sites")
		}
		p.Sites = append(p.Sites, site)
	}
	if len(p.Sites) > activitySitesMax {
		return p, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "too many sites (max 10)")
	}
	for _, name := range splitList(verbs) {
		verb, ok := model.ActivityVerbByName(name)
		if !ok {
			return p, apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "unknown verb in verbs")
		}
		p.Verbs = append(p.Verbs, verb)
	}
	before, he := decodeFeedCursor(cursor)
	if he != nil {
		return p, he
	}
	p.Before = before
	return p, nil
}

func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func encodeFeedCursor(at time.Time, id int64) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(strconv.FormatInt(at.UnixMicro(), 10) + ":" + strconv.FormatInt(id, 10)))
}

func decodeFeedCursor(s string) (*repository.ActivityCursor, *houseError) {
	if s == "" {
		return nil, nil
	}
	bad := apiErrMsg(http.StatusBadRequest, errors.ErrInvalidParam, "malformed cursor")
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, bad
	}
	at, id, ok := strings.Cut(string(raw), ":")
	if !ok {
		return nil, bad
	}
	micros, err1 := strconv.ParseInt(at, 10, 64)
	rowID, err2 := strconv.ParseInt(id, 10, 64)
	if err1 != nil || err2 != nil || rowID <= 0 {
		return nil, bad
	}
	return &repository.ActivityCursor{At: time.UnixMicro(micros), ID: rowID}, nil
}

func toActivityItemViews(rows []repository.ActivityItemRow) []dto.ActivityItemView {
	out := make([]dto.ActivityItemView, len(rows))
	for i := range rows {
		r := &rows[i]
		out[i] = dto.ActivityItemView{
			ID: r.ID, Site: r.Site, Key: r.Key, ActorID: r.ActorID, Verb: verbName(r.Verb),
			ObjectKind: r.ObjectKind, ObjectLabel: r.ObjectLabel, Title: r.Title, Excerpt: r.Excerpt,
			URL: r.URL, CoverImageHash: r.CoverImageHash, WorkID: r.WorkID,
			ContentLimit: contentLimitName(r.ContentLimit), OccurredAt: r.OccurredAt,
		}
	}
	return out
}

func verbName(v int16) string {
	if int(v) < len(model.ActivityVerbNames) && v >= 0 {
		return model.ActivityVerbNames[v]
	}
	return ""
}

func contentLimitName(c int16) string {
	if c == model.ContentLimitSFW {
		return "sfw"
	}
	return "nsfw"
}

func followNotifyName(level int16) string {
	return model.FollowNotifyNames[level]
}
