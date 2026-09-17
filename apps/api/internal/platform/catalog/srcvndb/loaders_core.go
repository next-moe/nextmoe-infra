package srcvndb

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

func newVNLoader(tx *gorm.DB, now time.Time) tableLoader {
	return newLoader(tx, func(get getter) (VN, bool) {
		id, _ := get("id")
		return VN{
			ID: id, OLang: getStr(get, "olang"), Image: getStr(get, "image"),
			CImage: getStr(get, "c_image"), Description: getStr(get, "description"),
			CVotecount: getInt(get, "c_votecount"), CRating: getFloat64Ptr(get, "c_rating"),
			CAverage: getFloat64Ptr(get, "c_average"), CLength: getIntPtr(get, "c_length"),
			CLengthnum: getInt(get, "c_lengthnum"), Length: getInt16(get, "length"),
			Devstatus: getInt16(get, "devstatus"), Alias: getStr(get, "alias"),
			IngestedAt: now,
		}, true
	})
}

func newVNTitleLoader(tx *gorm.DB, _ time.Time) tableLoader {
	return newLoader(tx, func(get getter) (VNTitle, bool) {
		id, _ := get("id")
		lang, _ := get("lang")
		return VNTitle{
			ID: id, Lang: lang, Official: getBool(get, "official"),
			Title: getStr(get, "title"), Latin: getStr(get, "latin"),
		}, true
	})
}

func newVNRelationLoader(tx *gorm.DB, _ time.Time) tableLoader {
	return newLoader(tx, func(get getter) (VNRelation, bool) {
		id, _ := get("id")
		vid, _ := get("vid")
		return VNRelation{
			ID: id, VID: vid,
			Relation: getStr(get, "relation"), Official: getBool(get, "official"),
		}, true
	})
}

func newCharLoader(tx *gorm.DB, now time.Time) tableLoader {
	return newLoader(tx, func(get getter) (Char, bool) {
		id, _ := get("id")
		return Char{
			ID: id, Image: getStr(get, "image"),
			BloodT: getStr(get, "bloodt"), CupSize: getStr(get, "cup_size"),
			Sex: getStr(get, "sex"), SpoilSex: getStr(get, "spoil_sex"),
			Gender: getStr(get, "gender"), SpoilGender: getStr(get, "spoil_gender"),
			Main: getStr(get, "main"), MainSpoil: getInt16(get, "main_spoil"),
			SBust: getInt16(get, "s_bust"), SWaist: getInt16(get, "s_waist"),
			SHip: getInt16(get, "s_hip"), Birthday: getInt16(get, "birthday"),
			Height: getInt16(get, "height"),
			Weight: getInt16Ptr(get, "weight"), Age: getInt16Ptr(get, "age"),
			Description: getStr(get, "description"),
			IngestedAt:  now,
		}, true
	})
}

func newCharNameLoader(tx *gorm.DB, _ time.Time) tableLoader {
	return newLoader(tx, func(get getter) (CharName, bool) {
		id, _ := get("id")
		lang, _ := get("lang")
		return CharName{
			ID: id, Lang: lang,
			Name: getStr(get, "name"), Latin: getStr(get, "latin"),
		}, true
	})
}

func newCharVNLoader(tx *gorm.DB, _ time.Time) tableLoader {
	return newLoader(tx, func(get getter) (CharVN, bool) {
		id, _ := get("id")
		vid, _ := get("vid")
		return CharVN{
			ID: id, VID: vid, RID: getStr(get, "rid"),
			Role: getStr(get, "role"), Spoil: getInt16(get, "spoil"),
		}, true
	})
}

func newImageLoader(tx *gorm.DB, _ time.Time) tableLoader {
	return newLoader(tx, func(get getter) (Image, bool) {
		id, _ := get("id")
		if !strings.HasPrefix(id, "ch") && !strings.HasPrefix(id, "cv") {
			return Image{}, false
		}
		return Image{
			ID: id, Width: getInt(get, "width"), Height: getInt(get, "height"),
			VoteCount:      getInt(get, "c_votecount"),
			SexualAvg:      getInt16(get, "c_sexual_avg"),
			SexualStddev:   getInt16(get, "c_sexual_stddev"),
			ViolenceAvg:    getInt16(get, "c_violence_avg"),
			ViolenceStddev: getInt16(get, "c_violence_stddev"),
			Weight:         getInt16(get, "c_weight"),
		}, true
	})
}
