package c

import "github.com/paulo-raca/go-enums/enum"

// --- Equal-set parallel enums: the motivating case ---

type SqlSuit struct{ enum.StringEnum[SqlSuit] } // want SqlSuit:`enumMembers\(SqlDiamonds,SqlHearts,SqlSpades; diamonds,hearts,spades\)`

var (
	SqlHearts   = enum.New[SqlSuit]("hearts")
	SqlDiamonds = enum.New[SqlSuit]("diamonds")
	SqlSpades   = enum.New[SqlSuit]("spades")
)

type ApiSuit struct{ enum.StringEnum[ApiSuit] } // want ApiSuit:`enumMembers\(ApiDiamonds,ApiHearts,ApiSpades; diamonds,hearts,spades\)`

var (
	ApiHearts   = enum.New[ApiSuit]("hearts")
	ApiDiamonds = enum.New[ApiSuit]("diamonds")
	ApiSpades   = enum.New[ApiSuit]("spades")
)

// PartialSuit is missing "spades" — casts should be flagged.
type PartialSuit struct{ enum.StringEnum[PartialSuit] } // want PartialSuit:`enumMembers\(PartialDiamonds,PartialHearts; diamonds,hearts\)`

var (
	PartialHearts   = enum.New[PartialSuit]("hearts")
	PartialDiamonds = enum.New[PartialSuit]("diamonds")
)

// --- Int enums: NextInt simulation must line up with explicit values ---

type SqlColor struct{ enum.IntEnum[SqlColor] } // want SqlColor:`enumMembers\(SqlBlue,SqlGreen,SqlRed; 0,1,2\)`

var (
	SqlRed   = enum.NextInt[SqlColor]() // 0
	SqlGreen = enum.NextInt[SqlColor]() // 1
	SqlBlue  = enum.NextInt[SqlColor]() // 2
)

// ApiColor declares the same {0, 1, 2} through a New+NextInt+NextInt mix.
type ApiColor struct{ enum.IntEnum[ApiColor] } // want ApiColor:`enumMembers\(ApiBlue,ApiGreen,ApiRed; 0,1,2\)`

var (
	ApiRed   = enum.New[ApiColor](0)
	ApiGreen = enum.NextInt[ApiColor]() // 1
	ApiBlue  = enum.NextInt[ApiColor]() // 2
)

// DifferentColor: same names, different int values — casts should be flagged.
type DifferentColor struct{ enum.IntEnum[DifferentColor] } // want DifferentColor:`enumMembers\(DiffBlue,DiffGreen,DiffRed; 10,20,30\)`

var (
	DiffRed   = enum.New[DifferentColor](10)
	DiffGreen = enum.New[DifferentColor](20)
	DiffBlue  = enum.New[DifferentColor](30)
)

// --- Non-constant value: cast checks involving OpaqueSuit are skipped ---

var opaque = "runtime-decided"

type OpaqueSuit struct{ enum.StringEnum[OpaqueSuit] } // want OpaqueSuit:`enumMembers\(OpaqueHearts; values unknown\)`

var OpaqueHearts = enum.New[OpaqueSuit](opaque)

func casts() {
	// Equal sets: no diagnostic on any of the cast forms.
	_, _ = enum.LookupAs[ApiSuit](SqlHearts)
	_, _ = enum.As[ApiSuit](SqlHearts)
	_ = enum.MustAs[ApiSuit](SqlHearts)
	_ = enum.SameValues[SqlSuit, ApiSuit]()

	// Value-set mismatch: PartialSuit is missing "spades".
	_, _ = enum.LookupAs[PartialSuit](SqlHearts) // want `cannot cast SqlSuit to PartialSuit: value sets differ; only in SqlSuit: "spades"`
	_, _ = enum.As[PartialSuit](SqlHearts)       // want `cannot cast SqlSuit to PartialSuit: value sets differ; only in SqlSuit: "spades"`
	_ = enum.MustAs[PartialSuit](SqlHearts)      // want `cannot cast SqlSuit to PartialSuit: value sets differ; only in SqlSuit: "spades"`
	_ = enum.SameValues[SqlSuit, PartialSuit]()  // want `value sets of SqlSuit and PartialSuit differ; only in SqlSuit: "spades"`

	// Symmetric: the extra values are reported on whichever side has them.
	_ = enum.SameValues[PartialSuit, SqlSuit]() // want `value sets of PartialSuit and SqlSuit differ; only in SqlSuit: "spades"`

	// Int enums: NextInt sim matches explicit values (equal sets).
	_ = enum.MustAs[ApiColor](SqlRed)
	_ = enum.SameValues[SqlColor, ApiColor]()

	// Int enums with different value sets.
	_ = enum.MustAs[DifferentColor](SqlRed)         // want `cannot cast SqlColor to DifferentColor: value sets differ; only in SqlColor: 0, 1, 2; only in DifferentColor: 10, 20, 30`
	_ = enum.SameValues[SqlColor, DifferentColor]() // want `value sets of SqlColor and DifferentColor differ; only in SqlColor: 0, 1, 2; only in DifferentColor: 10, 20, 30`

	// Non-constant value: check is skipped (no diagnostic).
	_ = enum.SameValues[SqlSuit, OpaqueSuit]()

	// Cross-kind SameValues: caught here (cross-kind LookupAs/As/MustAs is a
	// compile error, so it can only bite via SameValues).
	_ = enum.SameValues[SqlSuit, SqlColor]() // want `SqlSuit and SqlColor can never have the same values: one is string-backed, the other int-backed`
}
