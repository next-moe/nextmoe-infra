package utils

import (
	"time"

	"github.com/go-playground/validator/v10"
)

var validate *validator.Validate

func init() {
	validate = validator.New()
	_ = validate.RegisterValidation("kun_name", func(fl validator.FieldLevel) bool {
		return IsValidName(fl.Field().String())
	})

	_ = validate.RegisterValidation("date_or_empty", func(fl validator.FieldLevel) bool {
		s := fl.Field().String()
		if s == "" {
			return true
		}
		for _, layout := range []string{"2006-01-02", "2006-01", "2006"} {
			if _, err := time.Parse(layout, s); err == nil {
				return true
			}
		}
		return false
	})
}

func Validate(s any) error {
	return validate.Struct(s)
}
