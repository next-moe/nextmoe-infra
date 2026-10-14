package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// seed registry: first-party machine inference over catalog facts
const sourceDerived int16 = 18

const minIntroRunes = 20

type stats struct {
	Extracted          int
	Inserted           int
	Updated            int
	Conflict           int
	RefusedNotVerbatim int
	RefusedNotChinese  int
	RefusedShort       int
	RefusedNameAbsent  int
	UnmatchedName      int
	CallErrors         int
	Touched            int
	PanelAdopted       int
	PanelKept          int
	PanelErrors        int
}

type writer struct {
	db      *gorm.DB
	apply   bool
	refresh bool
	st      *stats
	samples int
	shown   int
	touched []int64
	judge   panelJudge
}

// gated is one passage that survived every deterministic gate, ready to be
// written — or, when the character holds an incumbent, to be put to the panel.
type gated struct {
	WorkID  int64
	Target  rosterChar
	Passage string
	SrcHash string
	MTModel string
}

// gate runs one work's extraction results through the deterministic gates.
// Every passage passes the verbatim gate against the work intro before
// anything else is considered. Judging is NOT done here: the panel votes are
// batched across works, so gate only sorts the survivors into those that can
// be written straight away and those that need a verdict first.
func (w *writer) gate(cand candidateWork, found map[string]string, mtModel string) (ready, contested []gated) {
	byName := rosterIndex(cand.Roster)
	srcHash := hashText(cand.Intro)
	for name, passage := range found {
		w.st.Extracted++
		name = strings.TrimSpace(name)
		target, ok := byName[name]
		if !ok {
			target, ok = byName[squashSpace(name)]
		}
		if !ok {
			w.st.UnmatchedName++
			slog.Warn("extraction key is not a roster name", "work", cand.WorkID, "name", name)
			continue
		}
		passage = strings.TrimSpace(passage)
		if utf8.RuneCountInString(passage) < minIntroRunes {
			w.st.RefusedShort++
			continue
		}
		if !nameAppears(cand.Intro, target) {
			w.st.RefusedNameAbsent++
			slog.Warn("the work intro never names this character", "work", cand.WorkID,
				"character", target.CharacterID, "name", name)
			continue
		}
		if !verbatim(cand.Intro, passage) {
			w.st.RefusedNotVerbatim++
			slog.Warn("extraction failed the verbatim gate", "work", cand.WorkID,
				"character", target.CharacterID, "name", name)
			continue
		}
		if looksJapanese(passage) {
			w.st.RefusedNotChinese++
			slog.Warn("passage is Japanese prose, not a zh intro", "work", cand.WorkID,
				"character", target.CharacterID, "name", name)
			continue
		}
		g := gated{WorkID: cand.WorkID, Target: target, Passage: passage, SrcHash: srcHash, MTModel: mtModel}
		if target.Incumbent != "" && w.judge != nil {
			contested = append(contested, g)
			continue
		}
		if w.shown < w.samples {
			fmt.Printf("  sample: work=%d %s → %s\n", cand.WorkID, name, truncate(passage, 80))
			w.shown++
		}
		ready = append(ready, g)
	}
	return ready, contested
}

// rosterIndex keys the roster by name and, where it is unambiguous, by the
// name with its spaces taken out. Asked for 「河合 葉月」 a model answers
// 「河合葉月」 often enough to matter, and an exact-key lookup files that good
// passage in the unmatched bucket (6 of 10 extractions on the 2026-08-22 fill
// retry). Two roster names that collapse to the same key get no squashed entry
// at all: guessing there hangs the passage on the wrong character.
func rosterIndex(roster []rosterChar) map[string]rosterChar {
	byName := make(map[string]rosterChar, len(roster)*2)
	for _, r := range roster {
		byName[r.Name] = r
	}
	seen := make(map[string]int, len(roster))
	for _, r := range roster {
		seen[squashSpace(r.Name)]++
	}
	for _, r := range roster {
		k := squashSpace(r.Name)
		if _, taken := byName[k]; !taken && seen[k] == 1 {
			byName[k] = r
		}
	}
	return byName
}

var spaceStripper = strings.NewReplacer(" ", "", "\t", "", "\u3000", "")

func squashSpace(s string) string { return spaceStripper.Replace(s) }

// commit writes one gated passage: a fresh derived row, or a rewrite of the
// stale one in the refresh bucket.
func (w *writer) commit(ctx context.Context, g gated) {
	if !w.apply {
		return
	}
	if w.refresh {
		if w.rewrite(ctx, g.WorkID, g.Target, g.Passage, g.SrcHash, g.MTModel) {
			w.touched = append(w.touched, g.WorkID)
		}
		return
	}
	res := w.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "character_id"}, {Name: "lang"}, {Name: "source_id"}},
		DoNothing: true,
	}).Create(&model.CatalogCharacterIntro{
		CharacterID: g.Target.CharacterID,
		Lang:        "zh-Hans",
		Intro:       g.Passage,
		SourceID:    sourceDerived,
		Provenance:  model.IntroProvenanceMachine,
		SrcHash:     g.SrcHash,
		MTModel:     g.MTModel,
	})
	if res.Error != nil {
		w.st.CallErrors++
		slog.Warn("insert extracted intro", "work", g.WorkID, "character", g.Target.CharacterID, "err", res.Error)
		return
	}
	if res.RowsAffected == 0 {
		w.st.Conflict++
		return
	}
	w.st.Inserted++
	w.touched = append(w.touched, g.WorkID)
}

// rewrite replaces a stale derived row in place. The src_hash it was loaded
// with is the update guard: a character on two candidate works is offered once
// per work, and the first refresh moves the hash, so the second work's write
// becomes a no-op instead of overwriting it with a different work's passage.
// Nothing is deleted when the fresh intro yields no passage — the old excerpt
// stays until a later run can replace it, so no character loses its intro.
func (w *writer) rewrite(ctx context.Context, workID int64, target rosterChar, passage, srcHash, mtModel string) bool {
	res := w.db.WithContext(ctx).Model(&model.CatalogCharacterIntro{}).
		Where("id = ? AND src_hash = ?", target.DerivedID, target.DerivedHash).
		Updates(map[string]any{"intro": passage, "src_hash": srcHash, "mt_model": mtModel})
	if res.Error != nil {
		w.st.CallErrors++
		slog.Warn("refresh derived intro", "work", workID, "character", target.CharacterID, "err", res.Error)
		return false
	}
	if res.RowsAffected == 0 {
		w.st.Conflict++
		return false
	}
	w.st.Updated++
	return true
}

// voteDispatcher casts one round of votes and answers in the same order. It
// owns the batching and the concurrency; resolvePanel owns the semantics.
type voteDispatcher func(ctx context.Context, cmps []comparison) []comparisonResult

// resolvePanel takes every contested passage through three votes, one round at
// a time so a whole round can travel as one batch. Which passage is shown
// first alternates by round, so a position bias cannot decide the verdict. A
// vote that errors drops that character out of the round: the incumbent stays
// and the character remains a candidate, so a later run re-judges it.
func (w *writer) resolvePanel(ctx context.Context, contested []gated, dispatch voteDispatcher) []gated {
	if len(contested) == 0 {
		return nil
	}
	votes := make([][]panelVote, len(contested))
	failed := make([]bool, len(contested))
	for round := range panelVotes {
		cmps := make([]comparison, 0, len(contested))
		idx := make([]int, 0, len(contested))
		for i, g := range contested {
			if failed[i] {
				continue
			}
			cmps = append(cmps, comparison{
				Name: g.Target.Name, Incumbent: g.Target.Incumbent,
				Challenger: g.Passage, ChallengerFirst: round%2 == 1,
			})
			idx = append(idx, i)
		}
		if len(cmps) == 0 {
			break
		}
		res := dispatch(ctx, cmps)
		if len(res) != len(cmps) {
			for _, i := range idx {
				failed[i] = true
				w.st.PanelErrors++
			}
			slog.Warn("panel round answered the wrong number of votes", "want", len(cmps), "got", len(res))
			continue
		}
		for j, r := range res {
			i := idx[j]
			if r.Err != nil {
				failed[i] = true
				w.st.PanelErrors++
				slog.Warn("panel failed — incumbent stays, retryable", "work", contested[i].WorkID,
					"character", contested[i].Target.CharacterID, "err", r.Err)
				continue
			}
			votes[i] = append(votes[i], r.Vote)
		}
	}
	var adopted []gated
	for i, g := range contested {
		if failed[i] {
			continue
		}
		ok := panelVerdict(votes[i])
		slog.Info("panel verdict", "work", g.WorkID, "character", g.Target.CharacterID,
			"name", g.Target.Name, "adopted", ok, "votes", fmt.Sprintf("%v", votes[i]))
		if w.shown < w.samples {
			fmt.Printf("  panel: work=%d %s adopted=%v\n    A(现有): %s\n    B(提取): %s\n",
				g.WorkID, g.Target.Name, ok, truncate(g.Target.Incumbent, 70), truncate(g.Passage, 70))
			w.shown++
		}
		if !ok {
			w.st.PanelKept++
			w.recordKept(ctx, g)
			continue
		}
		w.st.PanelAdopted++
		adopted = append(adopted, g)
	}
	return adopted
}

// recordKept files a kept verdict so the next panel run skips this pair while
// both texts are unchanged. g.SrcHash is already hashText(work intro), and the
// candidate query recomputes both hashes in SQL — the two sides must agree, so
// both are sha256 over raw UTF-8 bytes.
func (w *writer) recordKept(ctx context.Context, g gated) {
	if !w.apply {
		return
	}
	err := w.db.WithContext(ctx).Exec(`INSERT INTO catalog_character_intro_panel_verdict
		(work_id, character_id, src_hash, incumbent_hash, judged_at)
		VALUES (?, ?, ?, ?, now())
		ON CONFLICT (work_id, character_id) DO UPDATE
		SET src_hash = EXCLUDED.src_hash, incumbent_hash = EXCLUDED.incumbent_hash,
		    judged_at = EXCLUDED.judged_at`,
		g.WorkID, g.Target.CharacterID, g.SrcHash, hashText(g.Target.Incumbent)).Error
	if err != nil {
		slog.Warn("record kept verdict", "work", g.WorkID, "character", g.Target.CharacterID, "err", err)
	}
}

func (w *writer) flushTouch(ctx context.Context) error {
	if err := repository.TouchWorks(ctx, w.db, w.touched); err != nil {
		return fmt.Errorf("touch works: %w", err)
	}
	w.st.Touched = len(w.touched)
	return nil
}

// verbatim reports whether the passage exists inside the intro once both are
// stripped of whitespace and common list/heading punctuation. The model may
// merge ADJACENT sentences of one character and drop bullets, so the check
// runs per line of the passage: every line must reappear contiguously.
func verbatim(intro, passage string) bool {
	haystack := normalizeForMatch(intro)
	for line := range strings.SplitSeq(passage, "\n") {
		needle := normalizeForMatch(line)
		if needle == "" {
			continue
		}
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}

// nameAppears requires the WORK INTRO (not the passage — the model is allowed
// to drop a 「名字」 heading, so good extracts often omit the name) to name the
// character: full name, zh alias, or a given-name segment (≥2 runes). The
// family name alone does NOT count — in the 100-work rehearsal the model
// attached a passage about 月森鈴 to her roster-mate 月森玲子, whose own name
// never appears in that intro; a surname match would have let that through.
func nameAppears(intro string, target rosterChar) bool {
	hay := normalizeForMatch(intro)
	for _, cand := range []string{target.Name, target.ZhName} {
		cand = strings.TrimSpace(cand)
		if cand == "" {
			continue
		}
		if full := normalizeForMatch(cand); full != "" && strings.Contains(hay, full) {
			return true
		}
		segs := strings.Fields(cand)
		for i, seg := range segs {
			if i == 0 && len(segs) > 1 {
				continue
			}
			n := normalizeForMatch(seg)
			if utf8.RuneCountInString(n) >= 2 && strings.Contains(hay, n) {
				return true
			}
		}
	}
	return false
}

var parenSpan = regexp.MustCompile(`[（(][^）)]*[）)]`)

// looksJapanese refuses a passage that is Japanese prose rather than Chinese.
// A zh-Hans work intro can still carry untranslated Japanese, and the verbatim
// gate is happy to quote it — the 2026-08-22 refresh rehearsal filed
// 「一方、兄の虎鉄は無名……」 as a Chinese character intro.
//
// The hiragana ratio alone does NOT separate the two: measured over the whole
// derived face, a Chinese intro carrying furigana readings reaches 29.7%
// (主人公藤宫晴真（ふじみや はるま），是上奈木（かみなぎ）学园的…) while real
// Japanese prose starts at 31.7%. Parenthesised readings are the whole
// difference, so they come out first; what is left is Chinese with at most a
// few kana names in it.
func looksJapanese(passage string) bool {
	s := parenSpan.ReplaceAllString(passage, "")
	var hira, total int
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if r >= 'ぁ' && r <= 'ゖ' {
			hira++
		}
	}
	return total > 0 && float64(hira)/float64(total) > 0.30
}

var matchStripper = strings.NewReplacer(
	" ", "", "\t", "", "\n", "", "\r", "", "　", "",
	"・", "", "、", "", "。", "", ",", "", ".", "",
	"「", "", "」", "", "『", "", "』", "", "【", "", "】", "",
	"(", "", ")", "", "(", "", ")", "", ":", "", ":", "",
	"~", "", "~", "", "-", "", "―", "", "…", "", "!", "", "!", "", "?", "", "?", "",
)

func normalizeForMatch(s string) string {
	return matchStripper.Replace(s)
}

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
