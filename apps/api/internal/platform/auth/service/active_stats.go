package service

import (
	"context"
	"time"

	"api/internal/platform/auth/dto"
)

func (s *AdminService) ActiveStats(ctx context.Context, days int) (*dto.ActiveStatsResponse, error) {
	if days < 1 {
		days = 30
	}
	if days > 90 {
		days = 90
	}

	loc := statsLocation()
	nowSh := time.Now().In(loc)
	today := time.Date(nowSh.Year(), nowSh.Month(), nowSh.Day(), 0, 0, 0, 0, loc)
	tomorrow := today.AddDate(0, 0, 1)
	startCurrent := today.AddDate(0, 0, -(days - 1))
	startPrev := today.AddDate(0, 0, -(2*days - 1))

	counts, err := s.activityRepo.ActiveCountsByDay(ctx, startCurrent, statsTimezone)
	if err != nil {
		return nil, err
	}
	unique, err := s.activityRepo.UniqueActive(ctx, startCurrent, tomorrow)
	if err != nil {
		return nil, err
	}
	prevUnique, err := s.activityRepo.UniqueActive(ctx, startPrev, startCurrent)
	if err != nil {
		return nil, err
	}
	since, err := s.activityRepo.FirstHour(ctx)
	if err != nil {
		return nil, err
	}

	todayKey := today.Format("2006-01-02")
	series := make([]dto.DailyActive, 0, days)
	sum, peakCount, todayCount := 0, 0, 0
	peakDate := ""
	for i := 0; i < days; i++ {
		key := startCurrent.AddDate(0, 0, i).Format("2006-01-02")
		c := counts[key]
		series = append(series, dto.DailyActive{Date: key, Count: c})
		sum += c
		if c > peakCount {
			peakCount, peakDate = c, key
		}
		if key == todayKey {
			todayCount = c
		}
	}

	growth := 0.0
	if prevUnique > 0 {
		growth = float64(unique-prevUnique) / float64(prevUnique) * 100
	}
	trackedSince := ""
	if since != nil {
		trackedSince = since.In(loc).Format("2006-01-02")
	}

	return &dto.ActiveStatsResponse{
		RangeDays: days,
		Timezone:  statsTimezone,
		Series:    series,
		Summary: dto.ActiveSummary{
			Today:            todayCount,
			DailyAverage:     round1(float64(sum) / float64(days)),
			PeakDate:         peakDate,
			PeakCount:        peakCount,
			Unique:           unique,
			PrevPeriodUnique: prevUnique,
			GrowthPercent:    round1(growth),
			TrackedSince:     trackedSince,
		},
	}, nil
}

func (s *AdminService) HourlyActive(ctx context.Context, date string) (*dto.HourlyStatsResponse, error) {
	loc := statsLocation()
	day, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return nil, err
	}

	counts, err := s.activityRepo.ActiveCountsByHour(ctx, day, day.AddDate(0, 0, 1), statsTimezone)
	if err != nil {
		return nil, err
	}
	total, err := s.activityRepo.UniqueActive(ctx, day, day.AddDate(0, 0, 1))
	if err != nil {
		return nil, err
	}

	series := make([]dto.HourlyRegistration, 0, 24)
	for h := 0; h < 24; h++ {
		series = append(series, dto.HourlyRegistration{Hour: h, Count: counts[h]})
	}
	return &dto.HourlyStatsResponse{
		Date:     date,
		Timezone: statsTimezone,
		Series:   series,
		Total:    total,
	}, nil
}
