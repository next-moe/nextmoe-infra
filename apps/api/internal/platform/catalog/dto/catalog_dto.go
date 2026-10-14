package dto

type Page[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
}
