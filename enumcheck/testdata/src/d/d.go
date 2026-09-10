package d

import (
	"c"

	"github.com/paulo-raca/go-enums/enum"
)

// Cross-package cast: c.SqlSuit's members and values arrive via analysis facts.
func casts() {
	_ = c.SqlHearts.MustAs[c.ApiSuit]()

	_ = c.SqlHearts.MustAs[c.PartialSuit]() // want `cannot cast c\.SqlSuit to c\.PartialSuit: missing in c\.PartialSuit: "spades"`

	// Widening across packages is total: not flagged.
	_ = c.PartialHearts.MustAs[c.SqlSuit]()

	_ = enum.SameValues[c.SqlSuit, c.PartialSuit]() // want `value sets of c\.SqlSuit and c\.PartialSuit differ; only in c\.SqlSuit: "spades"`
}
