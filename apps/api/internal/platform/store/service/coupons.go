package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"api/internal/platform/store/model"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxBatchCoupons   = 1000
	maxCouponFace     = 1_000_000
	maxCouponCodeLen  = 200
	maxBatchNameRunes = 100
	maxBatchNoteRunes = 1000
)

var (
	ErrBatchNotFound  = errors.New("store: coupon batch not found")
	ErrBatchNotDraft  = errors.New("store: coupon batch is already published")
	ErrCouponNotFound = errors.New("store: coupon not found")
)

// InputError is a request the operator can correct; its text is shown to them.
type InputError struct{ Msg string }

func (e *InputError) Error() string { return e.Msg }

func inputErr(format string, a ...any) error { return &InputError{Msg: fmt.Sprintf(format, a...)} }

type CouponInput struct {
	FaceValue int    `json:"face_value"`
	Code      string `json:"code"`
	ExpiresOn string `json:"expires_on"`
}

type CreateBatchInput struct {
	Name       string        `json:"name"`
	PeriodFrom string        `json:"period_from"`
	PeriodTo   string        `json:"period_to"`
	Note       string        `json:"note"`
	Coupons    []CouponInput `json:"coupons"`
}

func (s *Service) CreateCouponBatch(ctx context.Context, actor uint, in CreateBatchInput) (*model.CouponBatch, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || utf8.RuneCountInString(name) > maxBatchNameRunes {
		return nil, inputErr("批次名称必填，最多 %d 字", maxBatchNameRunes)
	}
	note := strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(note) > maxBatchNoteRunes {
		return nil, inputErr("备注最多 %d 字", maxBatchNoteRunes)
	}
	if checkRange(in.PeriodFrom, in.PeriodTo, MaxAdminRangeDays) != nil {
		return nil, inputErr("结算区间要是 YYYY-MM-DD 的 JST 日期，起不晚于止，跨度不超过 %d 天", MaxAdminRangeDays)
	}
	if len(in.Coupons) == 0 || len(in.Coupons) > maxBatchCoupons {
		return nil, inputErr("一批要有 1 到 %d 张券", maxBatchCoupons)
	}

	coupons := make([]model.Coupon, 0, len(in.Coupons))
	codes := make([]string, 0, len(in.Coupons))
	seen := make(map[string]bool, len(in.Coupons))
	var dups []string
	for i, c := range in.Coupons {
		code := strings.TrimSpace(c.Code)
		if code == "" || len(code) > maxCouponCodeLen || strings.ContainsAny(code, " \t\r\n") {
			return nil, inputErr("第 %d 张券的券码为空、超过 %d 个字符或含空白", i+1, maxCouponCodeLen)
		}
		if c.FaceValue <= 0 || c.FaceValue > maxCouponFace {
			return nil, inputErr("第 %d 张券的面额要在 1 到 %d 点之间", i+1, maxCouponFace)
		}
		var expires *string
		if c.ExpiresOn != "" {
			if _, ok := model.ParseJSTDay(c.ExpiresOn); !ok {
				return nil, inputErr("第 %d 张券的有效期要是 YYYY-MM-DD", i+1)
			}
			day := c.ExpiresOn
			expires = &day
		}
		if seen[code] {
			dups = append(dups, code)
			continue
		}
		seen[code] = true
		codes = append(codes, code)
		coupons = append(coupons, model.Coupon{FaceValue: c.FaceValue, Code: code, ExpiresOn: expires})
	}
	if len(dups) > 0 {
		return nil, inputErr("这批里有重复的券码：%s", sample(dups))
	}
	var existing []string
	if err := s.db.WithContext(ctx).Model(&model.Coupon{}).
		Where("code IN ?", codes).Pluck("code", &existing).Error; err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, inputErr("这些券码之前已经录入过：%s", sample(existing))
	}

	batch := &model.CouponBatch{
		Name: name, PeriodFrom: in.PeriodFrom, PeriodTo: in.PeriodTo, Note: note,
		Status: model.BatchDraft, CreatedBy: actor,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(batch).Error; err != nil {
			return err
		}
		for i := range coupons {
			coupons[i].BatchID = batch.ID
		}
		return tx.CreateInBatches(&coupons, 500).Error
	})
	if isUniqueViolation(err) {
		return nil, inputErr("有券码刚被另一批录入，请刷新后重试")
	}
	if err != nil {
		return nil, err
	}
	return batch, nil
}

type ValueCount struct {
	FaceValue int `json:"face_value"`
	Count     int `json:"count"`
	Allocated int `json:"allocated"`
	Delivered int `json:"delivered"`
}

type BatchSummary struct {
	model.CouponBatch
	CouponCount int          `json:"coupon_count"`
	Points      int64        `json:"points"`
	Allocated   int          `json:"allocated"`
	Delivered   int          `json:"delivered"`
	ByValue     []ValueCount `json:"by_value"`
}

func (s *Service) ListCouponBatches(ctx context.Context) ([]BatchSummary, error) {
	var batches []model.CouponBatch
	if err := s.db.WithContext(ctx).Order("id DESC").Find(&batches).Error; err != nil {
		return nil, err
	}
	ids := make([]int64, len(batches))
	for i, b := range batches {
		ids[i] = b.ID
	}
	counts, err := s.valueCounts(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]BatchSummary, len(batches))
	for i, b := range batches {
		out[i] = summarize(b, counts[b.ID])
	}
	return out, nil
}

func summarize(b model.CouponBatch, values []ValueCount) BatchSummary {
	out := BatchSummary{CouponBatch: b, ByValue: values}
	if out.ByValue == nil {
		out.ByValue = []ValueCount{}
	}
	for _, v := range values {
		out.CouponCount += v.Count
		out.Points += int64(v.FaceValue) * int64(v.Count)
		out.Allocated += v.Allocated
		out.Delivered += v.Delivered
	}
	return out
}

func (s *Service) valueCounts(ctx context.Context, batchIDs []int64) (map[int64][]ValueCount, error) {
	out := map[int64][]ValueCount{}
	if len(batchIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		BatchID int64
		ValueCount
	}
	err := s.db.WithContext(ctx).Raw(`
		SELECT batch_id, face_value, count(*) AS count, count(user_id) AS allocated,
		       count(delivered_at) AS delivered
		  FROM store_coupons WHERE batch_id IN ?
		 GROUP BY batch_id, face_value
		 ORDER BY batch_id, face_value DESC`, batchIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.BatchID] = append(out[r.BatchID], r.ValueCount)
	}
	return out, nil
}

type GrantInput struct {
	UserID    uint `json:"user_id"`
	FaceValue int  `json:"face_value"`
	Count     int  `json:"count"`
}

// PublishCouponBatch assigns the batch's coupons as the operator settled them
// (soonest-expiring codes first), freezes each account's share of the period's
// clicks next to what it received, and makes the codes visible to the owners.
// Coupons no grant covers stay with the operator.
func (s *Service) PublishCouponBatch(ctx context.Context, id int64, apps []AdminApp, grants []GrantInput, now time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var batch model.CouponBatch
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&batch).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrBatchNotFound
		}
		if err != nil {
			return err
		}
		if batch.Status != model.BatchDraft {
			return ErrBatchNotDraft
		}

		var coupons []model.Coupon
		if err := tx.Where("batch_id = ?", id).
			Order("expires_on ASC NULLS LAST, id ASC").Find(&coupons).Error; err != nil {
			return err
		}
		byFace := map[int][]int64{}
		var total int64
		for _, c := range coupons {
			byFace[c.FaceValue] = append(byFace[c.FaceValue], c.ID)
			total += int64(c.FaceValue)
		}

		uniques, err := s.clientUniques(ctx, clientIDsOf(apps), batch.PeriodFrom, batch.PeriodTo)
		if err != nil {
			return err
		}
		users, claims, _ := claimantsOf(apps, uniques)

		want := map[int]int{}
		seen := map[string]bool{}
		var live []GrantInput
		for _, g := range grants {
			if g.Count < 0 {
				return inputErr("张数不能是负数")
			}
			if g.Count == 0 {
				continue
			}
			if users[g.UserID] == nil {
				return inputErr("用户 #%d 名下没有参与分成的应用，不能分给这个用户", g.UserID)
			}
			if _, ok := byFace[g.FaceValue]; !ok {
				return inputErr("这批没有 %d 点的券", g.FaceValue)
			}
			key := fmt.Sprintf("%d/%d", g.UserID, g.FaceValue)
			if seen[key] {
				return inputErr("用户 #%d 的 %d 点券重复出现", g.UserID, g.FaceValue)
			}
			seen[key] = true
			want[g.FaceValue] += g.Count
			live = append(live, g)
		}
		for face, n := range want {
			if n > len(byFace[face]) {
				return inputErr("%d 点的券只有 %d 张，却分出了 %d 张", face, len(byFace[face]), n)
			}
		}

		slices.SortFunc(live, func(a, b GrantInput) int {
			return cmp.Or(cmp.Compare(a.UserID, b.UserID), cmp.Compare(b.FaceValue, a.FaceValue))
		})
		next := map[int]int{}
		received := map[uint]int64{}
		for _, g := range live {
			ids := byFace[g.FaceValue][next[g.FaceValue] : next[g.FaceValue]+g.Count]
			next[g.FaceValue] += g.Count
			if err := tx.Model(&model.Coupon{}).Where("id IN ?", ids).
				Update("user_id", g.UserID).Error; err != nil {
				return err
			}
			received[g.UserID] += int64(g.FaceValue) * int64(g.Count)
		}

		ent := Entitlements(claims, total)
		var shares []model.CouponShare
		for _, c := range claims {
			if c.Uniques == 0 && received[c.UserID] == 0 {
				continue
			}
			u := users[c.UserID]
			shares = append(shares, model.CouponShare{
				BatchID: id, UserID: c.UserID, UserName: u.name, Apps: u.apps,
				Uniques: c.Uniques, SharePPM: ent[c.UserID].SharePPM,
				EntitledPoints: ent[c.UserID].Points, AllocatedPoints: received[c.UserID],
			})
		}
		if len(shares) > 0 {
			if err := tx.Create(&shares).Error; err != nil {
				return err
			}
		}
		return tx.Model(&batch).Updates(map[string]any{
			"status": model.BatchPublished, "published_at": now,
		}).Error
	})
}

func (s *Service) DeleteCouponBatch(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var batch model.CouponBatch
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&batch).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrBatchNotFound
		}
		if err != nil {
			return err
		}
		if batch.Status != model.BatchDraft {
			return ErrBatchNotDraft
		}
		if err := tx.Where("batch_id = ?", id).Delete(&model.Coupon{}).Error; err != nil {
			return err
		}
		return tx.Delete(&batch).Error
	})
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func sample(codes []string) string {
	if len(codes) > 5 {
		return strings.Join(codes[:5], "、") + fmt.Sprintf(" 等 %d 个", len(codes))
	}
	return strings.Join(codes, "、")
}
