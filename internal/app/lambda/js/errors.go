package js

import "errors"

// ErrFunctionMissing is returned when a requested export does not exist.
var ErrFunctionMissing = errors.New("strategy function missing")
