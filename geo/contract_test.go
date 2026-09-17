package geo_test

import (
	"github.com/arandu-io/hesape/foundation"
	"github.com/arandu-io/hesape/geo"
)

var (
	_ foundation.Module   = (*geo.Module)(nil)
	_ foundation.Bootable = (*geo.Module)(nil)
)
