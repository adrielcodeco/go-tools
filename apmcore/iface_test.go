package apmcore_test

import (
	"github.com/adrielcodeco/go-tools/apmcore"
	"github.com/adrielcodeco/go-tools/gscore"
)

// Compile-time assertion: *gscore.Manager must satisfy apmcore.CloserRegistrar.
var _ apmcore.CloserRegistrar = (*gscore.Manager)(nil)
