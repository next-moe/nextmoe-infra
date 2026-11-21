package entityintromt

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type replyTranslator struct {
	calls int
	reply func(src string) string
}

func (r *replyTranslator) Translate(_ context.Context, text string, _ SourceLang, _ Glossary) (string, string, error) {
	r.calls++
	return r.reply(text), "mt", nil
}

func labelMachine(t *testing.T, labelID int64) (string, bool) {
	t.Helper()
	var rows []string
	require.NoError(t, testDB.Raw(`SELECT intro FROM catalog_label_intro
		WHERE label_id = ? AND lang = 'zh-Hans' AND provenance = 1`, labelID).Scan(&rows).Error)
	if len(rows) == 0 {
		return "", false
	}
	return rows[0], true
}

func TestALinksOnlyLabelLosesItsMachineRowWithoutAModelCall(t *testing.T) {
	clean(t)
	vndb, _ := srcIDs(t)
	la := mkLabel(t, "kaguya")
	mkLabelIntro(t, la, "en", "Twitter | YouTube | Tumblr", vndb)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_label_intro
		(label_id, lang, intro, source_id, provenance, src_hash, mt_model, created_at, updated_at)
		VALUES (?, 'zh-Hans', '你好,kaguya。你似乎还没有提供需要翻译的英文条目简介正文。', ?, 1, 'h', 'mt', now(), now())`,
		la, vndb).Error)

	tr := &replyTranslator{reply: func(string) string { return "不该被调用" }}
	st, err := Run(context.Background(), tr, Opts{DSN: testDSN, Apply: true, Lane: LaneLabel})
	require.NoError(t, err)
	assert.Equal(t, 1, st[0].Untranslatable)
	assert.Equal(t, 1, st[0].Cleared)
	assert.Zero(t, tr.calls)
	_, ok := labelMachine(t, la)
	assert.False(t, ok)
}

func TestAPromptEchoIsRefusedAndRemembered(t *testing.T) {
	clean(t)
	require.NoError(t, testDB.Exec("TRUNCATE catalog_intro_mt_rejection").Error)
	vndb, _ := srcIDs(t)
	ch := mkCharacter(t, "John")
	mkCharIntro(t, ch, "en", "John's oldest child, who left the village ten years ago.", vndb)

	tr := &replyTranslator{reply: func(string) string {
		return "You are an expert translator specializing in game localization, specifically for visual novels (galgame)."
	}}
	for range 2 {
		_, err := Run(context.Background(), tr, Opts{DSN: testDSN, Apply: true, Lane: LaneCharacter})
		require.NoError(t, err)
	}
	assert.Equal(t, 1, tr.calls, "the refused source waits instead of being asked every night")
	_, ok := readMachine(t, ch)
	assert.False(t, ok)
	var reason string
	require.NoError(t, testDB.Raw(`SELECT reason FROM catalog_intro_mt_rejection
		WHERE intro_table = 'catalog_character_intro' AND owner_id = ?`, ch).Scan(&reason).Error)
	assert.Equal(t, "prompt-echo", reason)
}
