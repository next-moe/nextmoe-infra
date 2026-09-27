package main

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strings"
	"time"

	"api/internal/platform/chat/content"
	"api/internal/platform/chat/dto"
	"api/internal/platform/chat/service"
)

var imagePath = regexp.MustCompile(`^/image/([0-9a-f]{64})(?:\.[a-z]+)?$`)

type pair struct{ low, high int64 }

type imageRef struct {
	Hash    string
	Sticker bool
}

// images resolves an old image to the hash chat will reference: a sticker
// keeps its hash (the sticker service keeps it alive), an upload is re-hosted
// under chat's own image client.
type images interface {
	resolve(ref imageRef) (dto.Media, bool)
}

type plan struct {
	Conversations []service.ImportConversation
	Stats         stats
}

type stats struct {
	Pairs, MergedPairs, SkippedRooms, SkippedDeletedUsers                                           int
	Messages, Tombstones, Photos, Stickers, ExternalImages, LostImages, Reactions, UnknownReactions int
}

func emojiKeys() map[string]string {
	out := map[string]string{}
	for _, r := range service.Reactions() {
		out[strings.ReplaceAll(r.Emoji, "️", "")] = r.Key
	}
	return out
}

func groupID(key string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return int64(h.Sum64()&0x3fffffffffffffff) + 1
}

// build turns the old rooms into one import per pair of people. deleted
// names the accounts that no longer exist; their pairs are left out, since
// the account purge would erase what the import wrote.
func build(sources []*legacySource, deleted map[int64]bool, img images) plan {
	var p plan
	keys := emojiKeys()
	roomPair := map[string]pair{}
	pairRooms := map[pair][]legacyRoom{}
	for _, src := range sources {
		for _, r := range src.Rooms {
			if len(r.Users) != 2 || r.Users[0] == r.Users[1] {
				p.Stats.SkippedRooms++
				continue
			}
			pr := pair{min(r.Users[0], r.Users[1]), max(r.Users[0], r.Users[1])}
			if deleted[pr.low] || deleted[pr.high] {
				p.Stats.SkippedDeletedUsers++
				continue
			}
			roomPair[fmt.Sprintf("%s:%d", r.Source, r.ID)] = pr
			pairRooms[pr] = append(pairRooms[pr], r)
		}
	}
	msgs := map[pair][]service.ImportMessage{}
	allRead := map[string]bool{}
	for _, src := range sources {
		for _, r := range src.Rooms {
			allRead[fmt.Sprintf("%s:%d", r.Source, r.ID)] = src.AllRead
		}
		for _, m := range src.Messages {
			roomKey := fmt.Sprintf("%s:%d", m.Source, m.RoomID)
			pr, ok := roomPair[roomKey]
			if !ok {
				continue
			}
			converted := convertMessage(m, pr, allRead[roomKey], keys, img, &p.Stats)
			msgs[pr] = append(msgs[pr], converted...)
		}
	}
	pairs := make([]pair, 0, len(pairRooms))
	for pr := range pairRooms {
		pairs = append(pairs, pr)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].low != pairs[j].low {
			return pairs[i].low < pairs[j].low
		}
		return pairs[i].high < pairs[j].high
	})
	for _, pr := range pairs {
		rooms := pairRooms[pr]
		sort.Slice(rooms, func(i, j int) bool { return rooms[i].Created.Before(rooms[j].Created) })
		sources := map[string]bool{}
		for _, r := range rooms {
			sources[r.Source] = true
		}
		if len(sources) > 1 {
			p.Stats.MergedPairs++
		}
		ms := msgs[pr]
		sort.SliceStable(ms, func(i, j int) bool { return ms[i].CreatedAt.Before(ms[j].CreatedAt) })
		p.Conversations = append(p.Conversations, service.ImportConversation{
			UserA: pr.low, UserB: pr.high, OriginSite: rooms[0].Source, CreatedAt: rooms[0].Created, Messages: ms,
		})
		p.Stats.Pairs++
	}
	return p
}

func convertMessage(m legacyMessage, pr pair, allRead bool, keys map[string]string, img images, st *stats) []service.ImportMessage {
	key := fmt.Sprintf("%s:%d", m.Source, m.ID)
	base := service.ImportMessage{SourceKey: key, SenderID: m.SenderID, CreatedAt: m.Created, OriginSite: m.Source, EditedAt: m.EditedAt}
	if m.ReplyToID != nil {
		base.ReplyToKey = fmt.Sprintf("%s:%d", m.Source, *m.ReplyToID)
	}
	readBy := m.ReadBy
	if allRead {
		readBy = []int64{pr.low, pr.high}
	}
	if m.Deleted {
		st.Tombstones++
		base.Deleted = true
		base.EditedAt = nil
		base.ReadThroughFor = readBy
		return []service.ImportMessage{base}
	}
	text, ents, embedded := fromMarkdown(m.Content)
	var photos []dto.Media
	for _, im := range embedded {
		match := imagePath.FindStringSubmatch(im.URL)
		if match == nil {
			st.ExternalImages++
			label := "[图片]"
			if text != "" {
				text += "\n"
			}
			url := im.URL
			ents = append(ents, content.Entity{Type: content.TypeTextLink, Offset: content.UTF16Len(text), Length: content.UTF16Len(label), URL: &url})
			text += label
			continue
		}
		sticker := im.Alt == "sticker"
		md, ok := img.resolve(imageRef{Hash: match[1], Sticker: sticker})
		if !ok {
			st.LostImages++
			continue
		}
		if sticker {
			st.Stickers++
		} else {
			st.Photos++
		}
		photos = append(photos, md)
	}
	if normalized, nents, err := content.Normalize(text, ents); err == nil {
		text, ents = normalized, nents
	} else if plain, _, perr := content.Normalize(m.Content, nil); perr == nil {
		text, ents = plain, nil
	}
	for _, r := range m.Reactions {
		k, ok := keys[strings.ReplaceAll(r.Emoji, "️", "")]
		if !ok {
			st.UnknownReactions++
			continue
		}
		st.Reactions++
		base.Reactions = append(base.Reactions, service.ImportReaction{UserID: r.UserID, Reaction: k, CreatedAt: r.Created})
	}
	if len(photos) == 0 {
		if text == "" {
			return nil
		}
		st.Messages++
		base.Text, base.Entities, base.ReadThroughFor = text, ents, readBy
		return []service.ImportMessage{base}
	}
	var out []service.ImportMessage
	var group *int64
	if len(photos) > 1 {
		g := groupID(key)
		group = &g
	}
	for i := range photos {
		part := base
		if i > 0 {
			part.SourceKey = fmt.Sprintf("%s#%d", key, i+1)
			part.Reactions = nil
			part.ReplyToKey = ""
		} else {
			part.Text, part.Entities = text, ents
		}
		mm := photos[i]
		part.Media = &mm
		part.MediaGroupID = group
		part.CreatedAt = m.Created.Add(time.Duration(i) * time.Microsecond)
		if i == len(photos)-1 {
			part.ReadThroughFor = readBy
		}
		st.Messages++
		out = append(out, part)
	}
	return out
}
