// Package migrate owns the kun_agent schema.
//
// 2026-10-28, first version: conversations (a message tree with a head
// pointer), messages, runs and personas for the agent platform. All tables are
// new. uq_agent_run_active allows one running run per conversation, so a
// second submission while a reply streams is refused by the database rather
// than by a check-then-insert race. The default mascot (site 0) is seeded only
// when no persona exists; sites get their own mascot from operators.
//
// 2026-10-29, chat UX: agent_conversation.starred (default false),
// agent_message.feedback (empty, up or down) and agent_persona.suggestions (jsonb,
// default '[]'; empty means the built-in suggestion groups). All additive with
// defaults, so existing rows need no backfill.
package migrate

import (
	"encoding/json"
	"fmt"

	"api/internal/platform/agent/model"

	"gorm.io/gorm"
)

func Run(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.Conversation{}, &model.Message{}, &model.Run{}, &model.Persona{}); err != nil {
		return fmt.Errorf("agent automigrate: %w", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_run_active
		ON agent_run (conversation_id) WHERE status = 'running'`).Error; err != nil {
		return fmt.Errorf("create uq_agent_run_active: %w", err)
	}
	return seedDefaultPersona(db)
}

func seedDefaultPersona(db *gorm.DB) error {
	var n int64
	if err := db.Model(&model.Persona{}).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	greetings, _ := json.Marshal([]string{
		"你好呀！想找什么样的作品？描述一下剧情、角色或者氛围就好～",
		"今天想聊点什么？找游戏、问资料都可以哦。",
	})
	return db.Create(&model.Persona{
		SiteID: 0, Kind: model.PersonaMascot, Name: "小萌",
		Prompt:    "你是 NextMoe 的看板娘「小萌」，活泼、友善，熟悉 galgame、同人游戏和 ACG。用简洁自然的中文回答，必要时使用工具查询资料库；不知道的事情直说不知道，不编造作品或信息。",
		Greetings: greetings, Active: true,
	}).Error
}
