package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const (
	feedTitleMax   = 200
	feedExcerptMax = 300
	feedLabelWork  = "作品"
	feedLabelFold  = "收藏夹"
	feedLabelLabel = "会社"
)

var beijing = time.FixedZone("CST", 8*3600)

// FeedItem is the body of a community activity, everything but its key,
// actor, revision and removed flag.
type FeedItem struct {
	Verb         string    `json:"verb"`
	ObjectKind   string    `json:"object_kind"`
	ObjectLabel  string    `json:"object_label"`
	Title        string    `json:"title,omitempty"`
	Excerpt      string    `json:"excerpt,omitempty"`
	WorkID       *int64    `json:"work_id,omitempty"`
	ObjectRef    string    `json:"object_ref"`
	ContentLimit string    `json:"content_limit"`
	OccurredAt   time.Time `json:"occurred_at"`
}

type feedRow struct {
	key      string
	uid      int64
	workID   *int64
	folderID *int64
	item     FeedItem
	baseline *int
}

func putFeedItemTx(tx *gorm.DB, r feedRow) error {
	body, err := json.Marshal(r.item)
	if err != nil {
		return err
	}
	return tx.Exec(`
		INSERT INTO catalog_activity_item (key, actor_uid, work_id, folder_id, item, baseline, removed, revision, pushed_revision, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, false, ?, 0, now())
		ON CONFLICT (key) DO UPDATE SET
		    item = EXCLUDED.item, baseline = EXCLUDED.baseline, removed = false,
		    revision = GREATEST(EXCLUDED.revision, catalog_activity_item.revision + 1), updated_at = now()`,
		r.key, r.uid, r.workID, r.folderID, string(body), r.baseline, time.Now().UnixMicro()).Error
}

func retractFeedItemsTx(tx *gorm.DB, where string, args ...any) error {
	return tx.Exec(`
		UPDATE catalog_activity_item SET removed = true,
		    revision = GREATEST(?, revision + 1), updated_at = now()
		 WHERE NOT removed AND (`+where+`)`, append([]any{time.Now().UnixMicro()}, args...)...).Error
}

type feedPrivacy struct{ playPublic, companiesPublic bool }

func feedPrivacyTx(tx *gorm.DB, uid int64) (feedPrivacy, error) {
	var rows []model.CatalogUserPrivacy
	if err := tx.Where("actor_uid = ?", uid).Limit(1).Find(&rows).Error; err != nil {
		return feedPrivacy{}, err
	}
	if len(rows) == 0 {
		return feedPrivacy{playPublic: true}, nil
	}
	return feedPrivacy{playPublic: !rows[0].PlayRecordsPrivate, companiesPublic: rows[0].FollowedCompaniesPublic}, nil
}

func feedWorkTx(tx *gorm.DB, workID int64) (title, contentLimit string, err error) {
	var w model.CatalogWork
	if err := tx.Select("display_name", "content_rating").Take(&w, "id = ?", workID).Error; err != nil {
		return "", "", err
	}
	contentLimit = "nsfw"
	if w.ContentRating == model.ContentRatingAllAges {
		contentLimit = "sfw"
	}
	return feedText(w.DisplayName, feedTitleMax), contentLimit, nil
}

func feedText(s string, maxRunes int) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if utf8.RuneCountInString(s) > maxRunes {
		s = string([]rune(s)[:maxRunes])
	}
	return s
}

var workStateFeedNames = map[int16]string{
	model.WorkStateWish: "wish", model.WorkStateDoing: "doing", model.WorkStateDone: "done",
	model.WorkStateOnHold: "on_hold", model.WorkStateDropped: "dropped",
}

func workStateFeedKey(uid, workID int64, state string) string {
	return fmt.Sprintf("ws:%d:%d:%s", uid, workID, state)
}

// A state set again (done, doing, done) restores its earlier item and moves
// it to today; the items of the other states stay.
func feedWorkStateTx(tx *gorm.DB, uid, workID int64, state int16) error {
	p, err := feedPrivacyTx(tx, uid)
	if err != nil || !p.playPublic {
		return err
	}
	name, ok := workStateFeedNames[state]
	if !ok {
		return nil
	}
	title, limit, err := feedWorkTx(tx, workID)
	if err != nil {
		return err
	}
	return putFeedItemTx(tx, feedRow{
		key: workStateFeedKey(uid, workID, name), uid: uid, workID: &workID,
		item: FeedItem{
			Verb: "mark", ObjectKind: "work_" + name, ObjectLabel: feedLabelWork, Title: title,
			WorkID: &workID, ObjectRef: fmt.Sprintf("work:%d", workID), ContentLimit: limit, OccurredAt: time.Now(),
		},
	})
}

func retractWorkStateFeedTx(tx *gorm.DB, uid, workID int64) error {
	return retractFeedItemsTx(tx, "actor_uid = ? AND work_id = ? AND key LIKE 'ws:%'", uid, workID)
}

func playtimeTotalTx(tx *gorm.DB, uid, workID int64) (int, error) {
	var total int
	err := tx.Raw(`SELECT COALESCE(max(minutes), 0) FROM catalog_user_playtime WHERE actor_uid = ? AND work_id = ?`,
		uid, workID).Scan(&total).Error
	return total, err
}

// One item per work per Beijing day. baseline is the total the day started
// from, so the excerpt says how much the day added, whichever client said it.
func feedPlaytimeTx(tx *gorm.DB, uid, workID int64, before, after int) error {
	if after <= before {
		return nil
	}
	p, err := feedPrivacyTx(tx, uid)
	if err != nil || !p.playPublic {
		return err
	}
	now := time.Now()
	key := fmt.Sprintf("pt:%d:%d:%s", uid, workID, now.In(beijing).Format("20060102"))
	baseline := before
	var prior []model.CatalogActivityItem
	if err := tx.Select("baseline", "removed").Where("key = ?", key).Limit(1).Find(&prior).Error; err != nil {
		return err
	}
	if len(prior) == 1 && !prior[0].Removed && prior[0].Baseline != nil {
		baseline = *prior[0].Baseline
	}
	if after <= baseline {
		return nil
	}
	title, limit, err := feedWorkTx(tx, workID)
	if err != nil {
		return err
	}
	return putFeedItemTx(tx, feedRow{
		key: key, uid: uid, workID: &workID, baseline: &baseline,
		item: FeedItem{
			Verb: "play", ObjectKind: "work", ObjectLabel: feedLabelWork, Title: title,
			Excerpt: "当日 +" + feedMinutes(after-baseline) + " · 累计 " + feedMinutes(after),
			WorkID:  &workID, ObjectRef: fmt.Sprintf("work:%d", workID), ContentLimit: limit, OccurredAt: now,
		},
	})
}

func feedMinutes(m int) string {
	h, rest := m/60, m%60
	switch {
	case h == 0:
		return fmt.Sprintf("%d 分钟", rest)
	case rest == 0:
		return fmt.Sprintf("%d 小时", h)
	default:
		return fmt.Sprintf("%d 小时 %d 分钟", h, rest)
	}
}

func retractPlaytimeFeedTx(tx *gorm.DB, uid, workID int64) error {
	return retractFeedItemsTx(tx, "actor_uid = ? AND work_id = ? AND key LIKE 'pt:%'", uid, workID)
}

func folderFeedKey(folderID int64) string { return fmt.Sprintf("fd:%d", folderID) }

// A folder is announced once, the first time it is public: one made private
// and public again does not come back, and neither do the works it gained
// while hidden.
func feedFolderTx(tx *gorm.DB, f model.CatalogUserFolder) error {
	if f.Visibility != model.FolderVisibilityPublic {
		return nil
	}
	var n int64
	if err := tx.Model(&model.CatalogActivityItem{}).Where("key = ?", folderFeedKey(f.ID)).Count(&n).Error; err != nil || n > 0 {
		return err
	}
	id := f.ID
	return putFeedItemTx(tx, feedRow{
		key: folderFeedKey(f.ID), uid: f.OwnerUID, folderID: &id,
		item: FeedItem{
			Verb: "publish", ObjectKind: "folder", ObjectLabel: feedLabelFold, Title: feedText(f.Name, feedTitleMax),
			ObjectRef: fmt.Sprintf("folder:%d", f.ID), ContentLimit: "sfw", OccurredAt: time.Now(),
		},
	})
}

func retractFolderFeedTx(tx *gorm.DB, folderID int64) error {
	return retractFeedItemsTx(tx, "folder_id = ?", folderID)
}

func feedFolderItemTx(tx *gorm.DB, f model.CatalogUserFolder, workID int64) error {
	if f.Visibility != model.FolderVisibilityPublic {
		return nil
	}
	title, limit, err := feedWorkTx(tx, workID)
	if err != nil {
		return err
	}
	id := f.ID
	return putFeedItemTx(tx, feedRow{
		key: fmt.Sprintf("fi:%d:%d", f.ID, workID), uid: f.OwnerUID, workID: &workID, folderID: &id,
		item: FeedItem{
			Verb: "favorite", ObjectKind: "work", ObjectLabel: feedLabelWork, Title: title,
			Excerpt: feedText(f.Name, feedExcerptMax), WorkID: &workID,
			ObjectRef: fmt.Sprintf("work:%d", workID), ContentLimit: limit, OccurredAt: time.Now(),
		},
	})
}

func retractFolderItemFeedTx(tx *gorm.DB, folderID, workID int64) error {
	return retractFeedItemsTx(tx, "key = ?", fmt.Sprintf("fi:%d:%d", folderID, workID))
}

func companyFollowFeedKey(uid, companyID int64) string {
	return fmt.Sprintf("cf:%d:%d", uid, companyID)
}

func feedCompanyFollowTx(tx *gorm.DB, uid, companyID int64) error {
	p, err := feedPrivacyTx(tx, uid)
	if err != nil || !p.companiesPublic {
		return err
	}
	var label model.CatalogLabel
	if err := tx.Select("display_name").Take(&label, "id = ?", companyID).Error; err != nil {
		return err
	}
	return putFeedItemTx(tx, feedRow{
		key: companyFollowFeedKey(uid, companyID), uid: uid,
		item: FeedItem{
			Verb: "follow", ObjectKind: "company", ObjectLabel: feedLabelLabel, Title: feedText(label.DisplayName, feedTitleMax),
			ObjectRef: fmt.Sprintf("company:%d", companyID), ContentLimit: "sfw", OccurredAt: time.Now(),
		},
	})
}

func retractCompanyFollowFeedTx(tx *gorm.DB, uid, companyID int64) error {
	return retractFeedItemsTx(tx, "key = ?", companyFollowFeedKey(uid, companyID))
}

func retractPlayRecordFeedTx(tx *gorm.DB, uid int64) error {
	return retractFeedItemsTx(tx, "actor_uid = ? AND (key LIKE 'ws:%' OR key LIKE 'pt:%')", uid)
}

func retractCompanyFollowsFeedTx(tx *gorm.DB, uid int64) error {
	return retractFeedItemsTx(tx, "actor_uid = ? AND key LIKE 'cf:%'", uid)
}
