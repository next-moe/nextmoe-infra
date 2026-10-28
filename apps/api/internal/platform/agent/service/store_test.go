package service

import (
	"context"
	"errors"
	"os"
	"testing"

	agentmigrate "api/internal/platform/agent/migrate"
	"api/internal/platform/agent/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("agent service")
		os.Exit(0)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		panic(err)
	}
	if err := agentmigrate.Run(db); err != nil {
		panic(err)
	}
	testDB = db
	os.Exit(m.Run())
}

func resetAgent(t *testing.T) {
	t.Helper()
	for _, tbl := range []string{"agent_run", "agent_message", "agent_conversation"} {
		if err := testDB.Exec("TRUNCATE " + tbl + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestStartRunIsIdempotentAndSingleFlight(t *testing.T) {
	resetAgent(t)
	ctx := context.Background()
	s := NewStore(testDB)
	c, err := s.CreateConversation(ctx, 7, 14, model.ModeAssistant, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := StartRequest{Conversation: c, Parts: []Part{{Type: "text", Text: "你好"}}, Mode: model.ModeAssistant, IdemKey: "k1", Price: 1}
	run, created, err := s.StartRun(ctx, req)
	if err != nil || !created {
		t.Fatalf("start: created=%v err=%v", created, err)
	}
	again, created, err := s.StartRun(ctx, req)
	if err != nil || created || again.ID != run.ID {
		t.Fatalf("replay must return the same run: %+v created=%v err=%v", again, created, err)
	}
	changed := req
	changed.Parts = []Part{{Type: "text", Text: "别的"}}
	if _, _, err := s.StartRun(ctx, changed); !errors.Is(err, ErrIdempotencyUsed) {
		t.Fatalf("reused key with another body: err = %v", err)
	}
	other := req
	other.IdemKey = "k2"
	if _, _, err := s.StartRun(ctx, other); !errors.Is(err, ErrRunActive) {
		t.Fatalf("second run while one streams: err = %v", err)
	}

	if err := s.FinishRun(ctx, run, Finish{Status: model.RunCompleted, Parts: []Part{{Type: "text", Text: "嗨"}}, Settle: model.SettleFree, FreeUnits: 1}); err != nil {
		t.Fatal(err)
	}
	c, _ = s.Conversation(ctx, 7, 14, c.ID)
	branch, err := s.Branch(ctx, c.HeadMessageID)
	if err != nil || len(branch) != 2 || branch[0].Role != model.RoleUser || branch[1].Status != model.MessageComplete {
		t.Fatalf("branch = %+v err=%v", branch, err)
	}

	regen := req
	root := int64(0)
	regen.IdemKey, regen.ParentID = "k3", &root
	if _, _, err := s.StartRun(ctx, regen); err != nil {
		t.Fatalf("a new branch from the root: %v", err)
	}
	counts, _ := s.SiblingCounts(ctx, c.ID)
	if counts[0] != 2 {
		t.Fatalf("two root user messages expected, counts=%v", counts)
	}
	if runs, err := s.InterruptOrphans(ctx); err != nil || len(runs) != 1 || runs[0].Status != model.RunInterrupted {
		t.Fatalf("interrupt orphans: runs=%+v err=%v", runs, err)
	}
	if _, err := s.Conversation(ctx, 8, 14, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("another user must not see the conversation")
	}
}

func TestPersonaFallsBackToTheDefaultMascot(t *testing.T) {
	ctx := context.Background()
	s := NewStore(testDB)
	p, err := s.Persona(ctx, 999)
	if err != nil || p.SiteID != 0 {
		t.Fatalf("default persona: %+v err=%v", p, err)
	}
}
