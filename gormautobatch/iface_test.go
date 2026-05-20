package autobatch_test

import (
	"github.com/adrielcodeco/go-tools/gormautobatch"
	"github.com/adrielcodeco/go-tools/gscore"
)

// Compile-time assertion: *gscore.Manager must satisfy autobatch.CloserRegistrar.
var _ autobatch.CloserRegistrar = (*gscore.Manager)(nil)
