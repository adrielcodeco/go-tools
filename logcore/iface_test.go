package logcore_test

import (
	"github.com/adrielcodeco/go-tools/gscore"
	"github.com/adrielcodeco/go-tools/logcore"
)

// Compile-time assertion: *gscore.Manager must satisfy logcore.CloserRegistrar.
var _ logcore.CloserRegistrar = (*gscore.Manager)(nil)
