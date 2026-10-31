package dto

type RegistrationStatsRequest struct {
	Days int `query:"days" validate:"omitempty,min=1,max=90"`
}

type HourlyStatsRequest struct {
	Date string `query:"date" validate:"required,datetime=2006-01-02"`
}

type DailyRegistration struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type RegistrationSummary struct {
	Total           int     `json:"total"`
	DailyAverage    float64 `json:"daily_average"`
	PeakDate        string  `json:"peak_date"`
	PeakCount       int     `json:"peak_count"`
	Today           int     `json:"today"`
	TotalAllTime    int64   `json:"total_all_time"`
	PrevPeriodTotal int     `json:"prev_period_total"`
	GrowthPercent   float64 `json:"growth_percent"`
}

type RegistrationStatsResponse struct {
	RangeDays int                 `json:"range_days"`
	Timezone  string              `json:"timezone"`
	Series    []DailyRegistration `json:"series"`
	Summary   RegistrationSummary `json:"summary"`
}

type HourlyRegistration struct {
	Hour  int `json:"hour"`
	Count int `json:"count"`
}

type HourlyStatsResponse struct {
	Date     string               `json:"date"`
	Timezone string               `json:"timezone"`
	Series   []HourlyRegistration `json:"series"`
	Total    int                  `json:"total"`
}

type DailyActive struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type ActiveSummary struct {
	Today            int     `json:"today"`
	DailyAverage     float64 `json:"daily_average"`
	PeakDate         string  `json:"peak_date"`
	PeakCount        int     `json:"peak_count"`
	Unique           int     `json:"unique"`
	PrevPeriodUnique int     `json:"prev_period_unique"`
	GrowthPercent    float64 `json:"growth_percent"`
	TrackedSince     string  `json:"tracked_since,omitempty"`
}

type ActiveStatsResponse struct {
	RangeDays int           `json:"range_days"`
	Timezone  string        `json:"timezone"`
	Series    []DailyActive `json:"series"`
	Summary   ActiveSummary `json:"summary"`
}
