package service

import (
	"context"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/editspec"
)

func (s *PublicService) releaseLabelsFor(ctx context.Context, releaseIDs []int64) (map[int64][]dto.PublicWorkLabel, error) {
	out := make(map[int64][]dto.PublicWorkLabel, len(releaseIDs))
	if len(releaseIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ReleaseID   int64  `gorm:"column:release_id"`
		LabelID     int64  `gorm:"column:label_id"`
		DisplayName string `gorm:"column:display_name"`
		LabelKind   int16  `gorm:"column:label_kind"`
		Kind        int16  `gorm:"column:kind"`
		Lang        string `gorm:"column:lang"`
		LogoHash    string `gorm:"column:logo_hash"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT rl.release_id, rl.label_id, l.display_name, l.kind AS label_kind,
			rl.kind AS kind, l.lang, l.logo_hash
		FROM catalog_release_label rl
		JOIN catalog_label l ON l.id = rl.label_id AND l.deleted_at IS NULL
		WHERE rl.release_id IN ? AND `+editspec.NotSuppressedReleaseLabelSQL("rl")+`
		ORDER BY rl.release_id, rl.kind, l.display_name`, releaseIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	grouped := make(map[int64][]LabelAttribution, len(releaseIDs))
	for _, r := range rows {
		grouped[r.ReleaseID] = append(grouped[r.ReleaseID], LabelAttribution{
			LabelID: r.LabelID, DisplayName: r.DisplayName, LabelKind: r.LabelKind,
			Kind: r.Kind, Lang: r.Lang, LogoHash: r.LogoHash,
		})
	}
	for id, attrs := range grouped {
		out[id] = publicWorkLabels(attrs)
	}
	return out, nil
}

func (s *PublicService) attachReleaseLabels(ctx context.Context, releases []dto.PublicRelease) error {
	if len(releases) == 0 {
		return nil
	}
	ids := make([]int64, len(releases))
	for i, r := range releases {
		ids[i] = r.ID
	}
	byRelease, err := s.releaseLabelsFor(ctx, ids)
	if err != nil {
		return err
	}
	for i := range releases {
		if labels := byRelease[releases[i].ID]; labels != nil {
			releases[i].Labels = labels
		} else {
			releases[i].Labels = []dto.PublicWorkLabel{}
		}
	}
	return nil
}
