package service

import "api/internal/platform/chat/dto"

// Mirrors kun-galgame-forum app/constants/reaction.ts (the forum's topic
// reactions, Telegram's default set): a key added on one side must be added
// on the other, or a reaction made on one site renders as nothing on another.
var reactionVocabulary = []dto.ReactionOption{
	{Key: "like", Emoji: "👍", Label: "赞"},
	{Key: "dislike", Emoji: "👎", Label: "踩"},
	{Key: "heart", Emoji: "❤️", Label: "爱心"},
	{Key: "fire", Emoji: "🔥", Label: "火"},
	{Key: "party", Emoji: "🎉", Label: "庆祝"},
	{Key: "love", Emoji: "🥰", Label: "喜欢"},
	{Key: "clap", Emoji: "👏", Label: "鼓掌"},
	{Key: "thinking", Emoji: "🤔", Label: "思考"},
	{Key: "mindblown", Emoji: "🤯", Label: "震惊"},
	{Key: "scream", Emoji: "😱", Label: "尖叫"},
	{Key: "cry", Emoji: "😢", Label: "哭"},
	{Key: "pray", Emoji: "🙏", Label: "感谢"},
	{Key: "eyes", Emoji: "👀", Label: "关注"},
	{Key: "hundred", Emoji: "💯", Label: "满分"},
	{Key: "partyface", Emoji: "🥳", Label: "派对"},
	{Key: "starstruck", Emoji: "🤩", Label: "星星眼"},
	{Key: "angry", Emoji: "😠", Label: "生气"},
	{Key: "anxious", Emoji: "😰", Label: "紧张"},
	{Key: "banana", Emoji: "🍌", Label: "香蕉"},
	{Key: "eyebrow", Emoji: "🤨", Label: "挑眉"},
	{Key: "voltage", Emoji: "⚡", Label: "闪电"},
	{Key: "hotdog", Emoji: "🌭", Label: "热狗"},
	{Key: "hot", Emoji: "🥵", Label: "热"},
	{Key: "sob", Emoji: "😭", Label: "大哭"},
	{Key: "moai", Emoji: "🗿", Label: "摩艾"},
	{Key: "newmoon", Emoji: "🌚", Label: "黑月亮"},
	{Key: "police", Emoji: "🚓", Label: "警车"},
	{Key: "pouting", Emoji: "😡", Label: "怒"},
	{Key: "salute", Emoji: "🫡", Label: "敬礼"},
	{Key: "shrimp", Emoji: "🦐", Label: "虾"},
	{Key: "halo", Emoji: "😇", Label: "天使"},
	{Key: "sunglasses", Emoji: "😎", Label: "酷"},
	{Key: "whale", Emoji: "🐳", Label: "鲸鱼"},
}

var reactionKeys = func() map[string]bool {
	m := make(map[string]bool, len(reactionVocabulary))
	for _, r := range reactionVocabulary {
		m[r.Key] = true
	}
	return m
}()

func knownReaction(key string) bool { return reactionKeys[key] }

func Reactions() []dto.ReactionOption {
	return append([]dto.ReactionOption(nil), reactionVocabulary...)
}
