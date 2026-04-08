package controller

import "github.com/go-playground/validator/v10"

// CustomValidator is a custom validator for the echo framework
type CustomValidator struct {
	validator *validator.Validate
}

// Validate validates the input struct
func (cv *CustomValidator) Validate(i interface{}) error {
	return cv.validator.Struct(i)
}

// NewCustomValidator returns a custom validator struct
func NewCustomValidator() *CustomValidator {
	v := validator.New()
	return &CustomValidator{validator: v}
}
