package llm

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

var budgetZone = time.FixedZone("CST", 8*3600)

func budgetDay(t time.Time) string { return t.In(budgetZone).Format("20060102") }

func SceneSpendKey(scene string, day string) string { return "scene:" + scene + ":" + day }

func KeySpendKey(id int64, day string) string { return "key:" + strconv.FormatInt(id, 10) + ":" + day }

func (g *Gateway) checkBudget(ctx context.Context, s *snapshot, sc *Scene, accessKeyID int64) error {
	day := budgetDay(g.now())
	var keys []string
	if sc.DailyBudgetMicro > 0 {
		keys = append(keys, SceneSpendKey(sc.Key, day))
	}
	k := s.keyByID[accessKeyID]
	if k != nil && k.DailyBudgetMicro > 0 {
		keys = append(keys, KeySpendKey(k.ID, day))
	}
	if len(keys) == 0 {
		return nil
	}
	spent := g.coord.Spend(ctx, keys)
	if sc.DailyBudgetMicro > 0 && spent[SceneSpendKey(sc.Key, day)] >= sc.DailyBudgetMicro*1000 {
		return &Error{Kind: ErrKindBudget, Msg: fmt.Sprintf("scene %s has spent its daily budget of %s", sc.Key, usd(sc.DailyBudgetMicro))}
	}
	if k != nil && k.DailyBudgetMicro > 0 && spent[KeySpendKey(k.ID, day)] >= k.DailyBudgetMicro*1000 {
		return &Error{Kind: ErrKindBudget, Msg: fmt.Sprintf("access key %s has spent its daily budget of %s", k.Name, usd(k.DailyBudgetMicro))}
	}
	return nil
}

func (g *Gateway) addSpend(s *snapshot, sc *Scene, accessKeyID, nano int64) {
	if nano <= 0 || g.coord == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	day := budgetDay(g.now())
	total := g.coord.AddSpend(ctx, SceneSpendKey(sc.Key, day), nano)
	g.budgetAlert(ctx, "scene", sc.Key, sc.DailyBudgetMicro, total, day)
	if k := s.keyByID[accessKeyID]; k != nil {
		total := g.coord.AddSpend(ctx, KeySpendKey(k.ID, day), nano)
		g.budgetAlert(ctx, "access key", k.Name, k.DailyBudgetMicro, total, day)
	}
}

func (g *Gateway) budgetAlert(ctx context.Context, scope, name string, budgetMicro, spentNano int64, day string) {
	if budgetMicro <= 0 || g.db == nil {
		return
	}
	for _, pct := range []int64{80, 100} {
		if spentNano*100 < budgetMicro*1000*pct {
			continue
		}
		if !g.coord.Once(ctx, fmt.Sprintf("budget:%s:%s:%s:%d", scope, name, day, pct), 48*time.Hour) {
			continue
		}
		_ = g.db.Create(&RouteEvent{Kind: fmt.Sprintf("budget_%d", pct),
			Detail:    fmt.Sprintf("%s %s has spent %d%% of its daily budget %s", scope, name, pct, usd(budgetMicro)),
			CreatedAt: g.now()}).Error
	}
}

func usd(micro int64) string { return fmt.Sprintf("$%.2f", float64(micro)/1e6) }

type actorKey struct{}

func WithActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func ActorFrom(ctx context.Context) string {
	a, _ := ctx.Value(actorKey{}).(string)
	return a
}
