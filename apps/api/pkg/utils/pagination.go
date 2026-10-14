package utils

type Pagination struct {
	Page     int `json:"page" query:"page"`
	PageSize int `json:"page_size" query:"page_size"`
}

func (p *Pagination) Normalize() {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize < 1 {
		p.PageSize = 20
	}
	if p.PageSize > 100 {
		p.PageSize = 100
	}
}

func (p *Pagination) Offset() int {
	return (p.Page - 1) * p.PageSize
}

func (p *Pagination) Limit() int {
	return p.PageSize
}
