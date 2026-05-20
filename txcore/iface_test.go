package txcore_test

import (
	"github.com/adrielcodeco/go-tools/gscore"
	"github.com/adrielcodeco/go-tools/txcore"
)

// Compile-time assertion: *gscore.Manager must satisfy txcore.CloserRegistrar.
var _ txcore.CloserRegistrar = (*gscore.Manager)(nil)
