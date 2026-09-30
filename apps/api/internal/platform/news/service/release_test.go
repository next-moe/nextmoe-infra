package service

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/news/model"
)

const otherTrustedSource = "release_other"

func TestReleasePendingImportsMovesOnlyImportedPendingRowsOfOneSource(t *testing.T) {
	newFixture(t)
	if err := testDB.Exec(`
		INSERT INTO news_source (key, display_name, homepage_url, attribution, publisher_uid, column_url, active, auto_publish)
		VALUES (?, 'other', 'https://x', 'attr', 1, '', true, true)
		ON CONFLICT (key) DO NOTHING`, otherTrustedSource).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	fresh := insert(t, "1001", model.StatusPending, now, false)
	rejected := insert(t, "1002", model.StatusRejected, now, false)
	withdrawn := insert(t, "1003", model.StatusWithdrawn, now, false)
	dead := insert(t, "1004", model.StatusPending, now, true)
	draft := insert(t, nativeExternalIDPrefix+"0123456789abcdef0123456789abcdef", model.StatusPending, now, false)
	other := insert(t, "cv1#1", model.StatusPending, now, false)
	if err := testDB.Exec(`UPDATE news_item SET source_key = ? WHERE id = ?`,
		otherTrustedSource, other).Error; err != nil {
		t.Fatal(err)
	}

	n, err := ReleasePendingImports(context.Background(), testDB, model.SourceKeyYmgal, model.SystemActorUID, "standing release")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("released %d rows, want 1", n)
	}

	want := map[int64]int16{
		fresh:     model.StatusPublished,
		rejected:  model.StatusRejected,
		withdrawn: model.StatusWithdrawn,
		dead:      model.StatusPending,
		draft:     model.StatusPending,
		other:     model.StatusPending,
	}
	for id, status := range want {
		var got int16
		testDB.Raw(`SELECT status FROM news_item WHERE id = ?`, id).Scan(&got)
		if got != status {
			t.Errorf("item %d: status %d, want %d", id, got, status)
		}
	}

	var decisions []model.NewsModerationDecision
	if err := testDB.Find(&decisions).Error; err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decision rows = %d, want 1", len(decisions))
	}
	d := decisions[0]
	if d.ItemID != fresh || d.ActorUID != model.SystemActorUID ||
		d.FromStatus != model.StatusPending || d.ToStatus != model.StatusPublished || d.Reason != "standing release" {
		t.Errorf("decision = %+v", d)
	}
}

func TestReleasePendingImportsHonoursAutoPublishOff(t *testing.T) {
	newFixture(t)
	fresh := insert(t, "1101", model.StatusPending, time.Now().UTC().Truncate(time.Second), false)
	if err := testDB.Exec(`UPDATE news_source SET auto_publish = false WHERE key = ?`, model.SourceKeyYmgal).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testDB.Exec(`UPDATE news_source SET auto_publish = true WHERE key = ?`, model.SourceKeyYmgal)
	})
	n, err := ReleasePendingImports(context.Background(), testDB, model.SourceKeyYmgal, model.SystemActorUID, "standing release")
	if err != nil {
		t.Fatal(err)
	}
	var status int16
	testDB.Raw(`SELECT status FROM news_item WHERE id = ?`, fresh).Scan(&status)
	if n != 0 || status != model.StatusPending {
		t.Errorf("released=%d status=%d with auto_publish off, want 0/pending", n, status)
	}
}
