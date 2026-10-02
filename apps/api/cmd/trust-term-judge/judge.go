package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"api/internal/platform/catalog/llmsuggest"
)

const (
	verdictKeep   = "keep"
	verdictRetire = "retire"

	judgeAttempts  = 5
	judgeMaxTokens = 4096
)

const judgeSystem = `You are auditing a moderation word list for a Chinese-language community about galgames and visual novels: forum topics, game comments, download resources.

Every line below is one "suspect" term. When a post contains a suspect term as a substring the post is still published, but it is queued for a human moderator to look at. The list was imported in bulk from generic word lists, and some terms are ordinary words that fill the queue with innocent posts. Your job is to find those ordinary words; every other term stays.

Classify each term into exactly one category:

- ordinary: a common word or short phrase that normal members really do write in innocent posts about games, anime, daily life or the site itself — an everyday dictionary word, a common fragment of normal sentences, a plain number, a generic technical or internet word, a well-known place — with no insulting, sexual, commercial, political or illegal sense. Examples: 第一次, 管理员, 互联网, 服务器, 下载速度, 其中有, test, url, 500.
- insult: profanity, slurs, harassment, name-calling, including mild or casual insults.
- sexual: sexual acts, body parts, pornography, fetish or erotic wording, however common.
- solicitation: prostitution, escort and hook-up wording, including euphemisms and slang (for example 外送茶, 楼凤, 找小姐, 上门服务, 一夜情).
- commerce: advertising, spam, gambling, drugs, weapons, fraud, scam, account or document trading, contact-me wording; also anything regulated or typically sold through spam, even when the word itself is a plain noun: industrial chemicals and explosive precursors, medicines and medical conditions, surveillance, police or military gear, exam, certificate, tax and agency services.
- sensitive: politics, religion, ethnicity, extremism, violence, people in the news; government and party bodies, leaders and offices, and wording about how the country is run.
- unknown: anything else. A handle, code, domain, random-looking string or abbreviation; a person's name; a product, book or film title; a slogan; an odd, oddly specific or garbled phrase that would not turn up in ordinary conversation. Bulk lists carry such phrases because they are code words, deliberate misspellings or spam bait, so a phrase that merely looks harmless is unknown, not ordinary.

Only "ordinary" terms are retired, so use it only when you are sure the term is both common and innocent. A term with any insulting, sexual, commercial, political or illegal reading is not ordinary even if it also has an innocent one. When in doubt, choose unknown.

Answer with one entry per line of input, in the same order, copying each term exactly as written.`

var judgeCategories = []string{"ordinary", "insult", "sexual", "solicitation", "commerce", "sensitive", "unknown"}

const (
	categoryOrdinary = "ordinary"
	categoryUnjudged = "unjudged"
)

var judgeSchema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []string{"terms"},
	"properties": map[string]any{
		"terms": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"term", "category"},
				"properties": map[string]any{
					"term":     map[string]any{"type": "string"},
					"category": map[string]any{"type": "string", "enum": judgeCategories},
				},
			},
		},
	},
}

type candidate struct {
	ID   int64  `json:"id"`
	Term string `json:"term"`
}

type verdict struct {
	ID       int64  `json:"id"`
	Term     string `json:"term"`
	Verdict  string `json:"verdict"`
	Category string `json:"category"`
}

type chatter interface {
	ChatJSON(ctx context.Context, system, user, schemaName string, schema map[string]any, maxTokens int) (llmsuggest.ChatResult, error)
}

type judge struct {
	llm   chatter
	batch int
	pause func(attempt int)
}

func (j *judge) run(ctx context.Context, inPath, outPath string) error {
	cands, err := readJSONL[candidate](inPath)
	if err != nil {
		return err
	}
	done := map[int64]bool{}
	if prior, err := readJSONL[verdict](outPath); err == nil {
		for _, v := range prior {
			done[v.ID] = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var todo []candidate
	for _, c := range cands {
		if !done[c.ID] {
			todo = append(todo, c)
		}
	}
	out, err := os.OpenFile(outPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	enc := json.NewEncoder(out)

	fmt.Fprintf(os.Stderr, "%d candidates, %d already judged, %d to judge\n", len(cands), len(done), len(todo))
	retired := 0
	for start := 0; start < len(todo); start += j.batch {
		vs, err := j.judgeBatch(ctx, todo[start:min(start+j.batch, len(todo))])
		if err != nil {
			return fmt.Errorf("terms %d-%d: %w", start, start+j.batch, err)
		}
		for _, v := range vs {
			if v.Verdict == verdictRetire {
				retired++
			}
			if err := enc.Encode(v); err != nil {
				return err
			}
		}
		fmt.Fprintf(os.Stderr, "judged %d/%d, retire so far %d\n", min(start+j.batch, len(todo)), len(todo), retired)
	}
	return nil
}

func (j *judge) judgeBatch(ctx context.Context, terms []candidate) ([]verdict, error) {
	var user strings.Builder
	for _, c := range terms {
		user.WriteString(c.Term)
		user.WriteByte('\n')
	}
	var callErr, answerErr error
	for attempt := range judgeAttempts {
		if attempt > 0 {
			j.wait(attempt)
		}
		res, err := j.llm.ChatJSON(ctx, judgeSystem, user.String(), "term_verdicts", judgeSchema, judgeMaxTokens)
		if err != nil {
			callErr, answerErr = err, nil
			continue
		}
		callErr = nil
		if res.FinishReason == "length" {
			answerErr = errors.New("answer truncated")
			break
		}
		vs, err := parseVerdicts(terms, res.Content)
		if err != nil {
			answerErr = err
			continue
		}
		return vs, nil
	}
	if callErr != nil {
		return nil, fmt.Errorf("gave up after %d attempts: %w", judgeAttempts, callErr)
	}
	// A batch the model answers but cannot cover is narrowed until the term at
	// fault stands alone. The first full run stopped at term 9,080 on 私\\服:
	// the model wrote its two backslashes back as one, five times.
	if len(terms) == 1 {
		fmt.Fprintf(os.Stderr, "unjudged, kept: %q (%v)\n", terms[0].Term, answerErr)
		return []verdict{{ID: terms[0].ID, Term: terms[0].Term, Verdict: verdictKeep, Category: categoryUnjudged}}, nil
	}
	left, err := j.judgeBatch(ctx, terms[:len(terms)/2])
	if err != nil {
		return nil, err
	}
	right, err := j.judgeBatch(ctx, terms[len(terms)/2:])
	return append(left, right...), err
}

func (j *judge) wait(attempt int) {
	if j.pause != nil {
		j.pause(attempt)
		return
	}
	time.Sleep(time.Duration(attempt) * 10 * time.Second)
}

// The answer carries a category for every term and names each by its text.
// Both halves were learned on the calibration set. Asked for line numbers, the
// model drifted inside a 60-term batch and retired the banned escort term
// 外送茶 as "普通数字，无特殊含义", a verdict belonging to another line. Asked
// only for the list of terms to retire, it listed 65 of 74, the escort terms
// included, each as "could be innocent".
func parseVerdicts(terms []candidate, content string) ([]verdict, error) {
	var ans struct {
		Terms []struct {
			Term     string `json:"term"`
			Category string `json:"category"`
		} `json:"terms"`
	}
	if err := json.Unmarshal([]byte(content), &ans); err != nil {
		return nil, fmt.Errorf("decode answer: %w", err)
	}
	out := make([]verdict, len(terms))
	at := make(map[string]int, len(terms))
	for i, c := range terms {
		out[i] = verdict{ID: c.ID, Term: c.Term}
		at[c.Term] = i
	}
	for _, r := range ans.Terms {
		i, ok := at[r.Term]
		if !ok {
			return nil, fmt.Errorf("answer names %q, which is not in the batch", r.Term)
		}
		if out[i].Verdict != "" {
			return nil, fmt.Errorf("answer names %q twice", r.Term)
		}
		if !slices.Contains(judgeCategories, r.Category) {
			return nil, fmt.Errorf("answer files %q under %q", r.Term, r.Category)
		}
		out[i].Verdict, out[i].Category = verdictKeep, r.Category
		if r.Category == categoryOrdinary {
			out[i].Verdict = verdictRetire
		}
	}
	for _, v := range out {
		if v.Verdict == "" {
			return nil, fmt.Errorf("answer skips %q", v.Term)
		}
	}
	return out, nil
}

func readJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows []T
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; sc.Scan(); line++ {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var row T
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		rows = append(rows, row)
	}
	return rows, sc.Err()
}
