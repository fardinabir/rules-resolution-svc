package domain

import "errors"

// ErrNotFound is returned when an override lookup finds no record.
var ErrNotFound = errors.New("not found")
