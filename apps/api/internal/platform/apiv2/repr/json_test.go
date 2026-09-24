package repr

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"api/internal/platform/catalog/model"
)

const testHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestIDIsJSONString(t *testing.T) {
	w, ok := NewWork(19658, "galgame", "紅殻のパンドラ", "ja", "r18", "released",
		TimeUTC(time.Date(2024, 11, 3, 8, 12, 44, 0, time.UTC)),
		TimeUTC(time.Date(2026, 8, 19, 2, 31, 6, 0, time.UTC)),
		nil, nil, ptr("2011-06-24"), ptr("day"), nil, nil, nil, "nsfw")
	if !ok {
		t.Fatal("NewWork")
	}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"id":19658`) {
		t.Fatalf("id marshaled as number: %s", b)
	}
	if !strings.Contains(string(b), `"id":"19658"`) {
		t.Fatalf("id missing as string: %s", b)
	}
	if strings.Contains(string(b), `"kind"`) {
		t.Fatalf("kind must not appear: %s", b)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["localized"].(map[string]any); !ok {
		t.Fatalf("localized must be an object, got %T", raw["localized"])
	}
	if raw["claim"] != nil {
		t.Fatalf("unclaimed claim must be null, got %v", raw["claim"])
	}
	if raw["cover"] != nil || raw["banner"] != nil {
		t.Fatalf("missing images must be null")
	}
}

func TestImageNeverBareHash(t *testing.T) {
	img, ok := NewImage("https://img.nextmoe.dev/image", testHash, "vndb", ptrInt(800), ptrInt(1131), ptr("thumb"), ptrInt16(0), nil)
	if !ok || img == nil {
		t.Fatal("NewImage")
	}
	b, err := json.Marshal(img)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"url":"https://img.nextmoe.dev/image/01/23/`+testHash+`.webp"`) {
		t.Fatalf("url: %s", b)
	}
	if !strings.Contains(string(b), `"hash":"`+testHash+`"`) {
		t.Fatalf("hash: %s", b)
	}
	if !strings.Contains(string(b), `"sexual":"safe"`) {
		t.Fatalf("sexual: %s", b)
	}
	if !strings.Contains(string(b), `"violence":null`) {
		t.Fatalf("unassessed violence must be null: %s", b)
	}
	if strings.Contains(string(b), "cover_kind") {
		t.Fatal("cover_kind is not in the image type")
	}
}

func TestCoverHasRowIDNotKind(t *testing.T) {
	c, ok := NewCover("https://img.nextmoe.dev/image", testHash, "vndb", 48213, 7, true, ptrInt(800), ptrInt(1131), nil, ptrInt16(0), nil)
	if !ok || c == nil {
		t.Fatal("NewCover")
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"id":"48213"`) {
		t.Fatalf("cover id: %s", b)
	}
	// This test used to refuse the field "until that vocabulary is closed";
	// deviation 44 ships it as an explicitly OPEN vocabulary instead (the
	// source/platform convention), under the G8-safe name cover_kind.
	if !strings.Contains(string(b), `"cover_kind":""`) {
		t.Fatalf("cover must emit cover_kind, empty when unrecorded: %s", b)
	}
	if strings.Contains(string(b), `"kind"`) && !strings.Contains(string(b), `"cover_kind"`) {
		t.Fatalf("the bare kind property is forbidden by G8: %s", b)
	}
}

func TestListOmitsCursorOnLastPage(t *testing.T) {
	page := NewList([]Work{}, nil)
	b, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "next_cursor") {
		t.Fatalf("last page must omit next_cursor: %s", b)
	}
	if !strings.Contains(string(b), `"items":[]`) {
		t.Fatalf("empty items must be []: %s", b)
	}
	cur := Cursor("eyJpZCI6MTk2NTh9")
	page = NewList([]Work{{Object: "work", ID: "1", Localized: map[string]LocalizedText{}}}, &cur)
	b, err = json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"next_cursor":"cur_eyJpZCI6MTk2NTh9"`) {
		t.Fatalf("cursor: %s", b)
	}
}

func TestEnumsRejectUnknown(t *testing.T) {
	if _, ok := TitleKind(model.WorkTitleKindSearchHint); ok {
		t.Fatal("search_hint must not enter the public title_kind vocabulary")
	}
	if _, ok := CompanyKind(99); ok {
		t.Fatal("unknown company kind must not become other")
	}
	if _, ok := Medium(0); ok {
		t.Fatal("unknown medium")
	}
	g, ok := Gender(nil)
	if !ok || g != nil {
		t.Fatal("unrecorded gender is null")
	}
}

func ptrInt(n int) *int       { return &n }
func ptrInt16(n int16) *int16 { return &n }
