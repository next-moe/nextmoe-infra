package repr

type CalendarList struct {
	List[Work]
	Meta CalendarMeta `json:"meta" doc:"Calendar navigation block. Always present on this collection."`
}

type CalendarMeta struct {
	_        struct{} `json:"-" additionalProperties:"true"`
	Today    string   `json:"today" format:"date" maxLength:"10" doc:"Today in Asia/Tokyo, the calendar's home timezone."`
	MinMonth *string  `json:"min_month" pattern:"^[0-9]{4}-[0-9]{2}$" maxLength:"7" doc:"Earliest month with a dated release under the same filters. Only the dated month window carries it; null when that window's population has no dated release at all."`
	MaxMonth *string  `json:"max_month" pattern:"^[0-9]{4}-[0-9]{2}$" maxLength:"7" doc:"Latest month with a dated release under the same filters. Only the dated month window carries it; null when that window's population has no dated release at all."`
	HasPrev  *bool    `json:"has_prev" doc:"Whether a month before the requested one has dated releases. Only the dated month window carries it; null on the year-only and undated windows."`
	HasNext  *bool    `json:"has_next" doc:"Whether a month after the requested one has dated releases. Only the dated month window carries it; null on the year-only and undated windows."`
}
