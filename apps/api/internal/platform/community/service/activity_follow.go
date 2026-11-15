package service

import (
	"maps"
	"slices"
	"strconv"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

const (
	followActivityKind  = "user"
	followActivityLabel = "用户"
)

func followActivityKey(followerID, followeeID int64) string {
	return OwnActivityKeyPrefix + "follow:" + strconv.FormatInt(followerID, 10) + ":" + strconv.FormatInt(followeeID, 10)
}

// writeFollowActivityTx puts a new follow edge in the follower's feed under
// the site it was made on. Community knows no user names, so the item carries
// only object_ref and the reading site names the followee itself.
func writeFollowActivityTx(tx *gorm.DB, site string, followerID, followeeID int64) error {
	ref := "user:" + strconv.FormatInt(followeeID, 10)
	w := repository.ActivityWrite{
		Key: followActivityKey(followerID, followeeID), ActorID: followerID,
		Verb: model.ActivityVerbFollow, ObjectKind: followActivityKind, ObjectLabel: followActivityLabel,
		ObjectRef: &ref, ContentLimit: model.ContentLimitSFW, OccurredAt: time.Now(),
	}
	return putOwnActivitiesTx(tx, []string{w.Key}, map[string]siteWrite{w.Key: {site: site, write: w}})
}

func retractFollowActivitiesTx(tx *gorm.DB, edges [][2]int64) error {
	keys := make([]string, len(edges))
	for i, e := range edges {
		keys[i] = followActivityKey(e[0], e[1])
	}
	return putOwnActivitiesTx(tx, keys, nil)
}

type siteWrite struct {
	site  string
	write repository.ActivityWrite
}

// putOwnActivitiesTx writes live[key] where given and tombstones every other
// live copy of the keys, on any site. Revisions step past the stored ones, so
// a wall-clock step back cannot make the change stale.
func putOwnActivitiesTx(tx *gorm.DB, keys []string, live map[string]siteWrite) error {
	stored, err := repository.OwnActivitiesByKeyTx(tx, keys)
	if err != nil {
		return err
	}
	now := time.Now().UnixMicro()
	bySite := map[string][]activityJob{}
	for _, key := range keys {
		rev := now
		for _, row := range stored[key] {
			rev = max(rev, row.Revision+1)
		}
		lw, isLive := live[key]
		if isLive {
			lw.write.Revision = rev
			bySite[lw.site] = append(bySite[lw.site], activityJob{write: lw.write})
		}
		for _, row := range stored[key] {
			if row.Removed || (isLive && row.Site == lw.site) {
				continue
			}
			bySite[row.Site] = append(bySite[row.Site], activityJob{write: repository.ActivityWrite{
				Key: key, ActorID: row.ActorID, Verb: row.Verb, ObjectKind: row.ObjectKind,
				Revision: rev, Removed: true, OccurredAt: time.Now(),
			}})
		}
	}
	for _, site := range slices.Sorted(maps.Keys(bySite)) {
		jobs := bySite[site]
		results := make([]ActivityResult, len(jobs))
		for i := range jobs {
			jobs[i].idx = i
		}
		if err := writeActivitiesTx(tx, site, jobs, results); err != nil {
			return err
		}
	}
	return nil
}
