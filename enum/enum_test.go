package enum_test

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/paulo-raca/go-enums/enum"
	"github.com/stretchr/testify/require"
)

// Compile-time checks that the database/sql interfaces are satisfied (Valuer on
// the value, Scanner on the pointer), including via embedding promotion.
var (
	_ driver.Valuer = Suit{}
	_ sql.Scanner   = (*Suit)(nil)
	_ driver.Valuer = Color{}
	_ sql.Scanner   = (*Color)(nil)
)

// --- string enum under test ---------------------------------------------

type Suit struct{ enum.StringEnum[Suit] }

var (
	Hearts   = enum.New[Suit]("hearts")
	Diamonds = enum.New[Suit]("diamonds")
	Spades   = enum.New[Suit]("spades")
)

// --- int enums under test (iota-like, and explicit-start) ----------------

type Color struct{ enum.IntEnum[Color] }

var (
	Red   = enum.NextInt[Color]() // 0
	Green = enum.NextInt[Color]() // 1
	Blue  = enum.NextInt[Color]() // 2
)

type Level struct{ enum.IntEnum[Level] }

var (
	Low  = enum.New[Level](10)   // explicit start
	Mid  = enum.NextInt[Level]() // 11
	High = enum.NextInt[Level]() // 12
)

// Mixed interleaves auto and explicit values, including an explicit value below
// the current max, to pin down that NextInt yields max(allValues)+1.
type Mixed struct{ enum.IntEnum[Mixed] }

var (
	MixA = enum.NextInt[Mixed]() // 0
	MixB = enum.New[Mixed](100)  // 100
	MixC = enum.New[Mixed](50)   // 50  (below current max)
	MixD = enum.NextInt[Mixed]() // 101 = max(0,100,50)+1
	MixE = enum.New[Mixed](-5)   // -5  (negative, below max)
	MixF = enum.NextInt[Mixed]() // 102
)

func TestStringBasics(t *testing.T) {
	require.Equal(t, "hearts", Hearts.String())
	require.True(t, Hearts.IsValid())

	var zero Suit
	require.False(t, zero.IsValid(), "zero value must not be valid")

	got, ok := enum.Lookup[Suit]("spades")
	require.True(t, ok)
	require.Equal(t, Spades, got)

	_, ok = enum.Lookup[Suit]("nope")
	require.False(t, ok, "unknown string must miss")

	// package-level Valid takes the backing value, not the member.
	require.True(t, enum.Valid[Suit]("hearts"))
	require.False(t, enum.Valid[Suit]("nope"))
}

func TestStringValuesOrder(t *testing.T) {
	require.Equal(t, []Suit{Hearts, Diamonds, Spades}, enum.Values[Suit]())
}

func TestStringJSON(t *testing.T) {
	b, err := json.Marshal(Diamonds)
	require.NoError(t, err)
	require.Equal(t, `"diamonds"`, string(b))

	var r Suit
	require.NoError(t, json.Unmarshal([]byte(`"hearts"`), &r))
	require.Equal(t, Hearts, r)

	err = json.Unmarshal([]byte(`"bogus"`), &r)
	var invalid *enum.InvalidValueError[Suit]
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, "bogus", invalid.Value)
}

func TestStringAsJSONMapKey(t *testing.T) {
	b, err := json.Marshal(map[Suit]int{Spades: 3})
	require.NoError(t, err)
	require.JSONEq(t, `{"spades":3}`, string(b))
}

func TestIntAutoIncrement(t *testing.T) {
	require.Equal(t, []int{0, 1, 2}, []int{Red.Int(), Green.Int(), Blue.Int()})
	// explicit-start sequence continues from the explicit value
	require.Equal(t, []int{10, 11, 12}, []int{Low.Int(), Mid.Int(), High.Int()})
}

func TestIntNextIsMaxPlusOne(t *testing.T) {
	got := []int{MixA.Int(), MixB.Int(), MixC.Int(), MixD.Int(), MixE.Int(), MixF.Int()}
	require.Equal(t, []int{0, 100, 50, 101, -5, 102}, got)
}

func TestIndex(t *testing.T) {
	// 0-based registration order; zero value is -1.
	require.Equal(t, 0, Hearts.Index())
	require.Equal(t, 1, Diamonds.Index())
	require.Equal(t, 2, Spades.Index())

	var z Suit
	require.Equal(t, -1, z.Index())

	// IsValid / IsZero are inverses tracking the zero value.
	require.True(t, Hearts.IsValid())
	require.False(t, Hearts.IsZero())
	require.False(t, z.IsValid())
	require.True(t, z.IsZero())

	// Values is in registration order, and Index matches the slice index.
	for i, v := range enum.Values[Suit]() {
		require.Equalf(t, i, v.Index(), "Values[%d].Index()", i)
	}

	// Index survives a JSON round-trip (it is copied from the canonical member).
	b, err := json.Marshal(Diamonds)
	require.NoError(t, err)
	var got Suit
	require.NoError(t, json.Unmarshal(b, &got))
	require.Equal(t, 1, got.Index())
}

func TestOrder(t *testing.T) {
	// Compare follows registration order (Hearts, Diamonds, Spades).
	require.Negative(t, Hearts.Compare(Spades))
	require.Positive(t, Spades.Compare(Hearts))
	require.Zero(t, Diamonds.Compare(Diamonds))

	// Sortable straight from the method expression.
	xs := []Suit{Spades, Hearts, Diamonds}
	slices.SortFunc(xs, Suit.Compare)
	require.Equal(t, []Suit{Hearts, Diamonds, Spades}, xs)

	// IntEnum orders by registration position, not by int value.
	require.Positive(t, Blue.Compare(Red))
	require.Negative(t, Red.Compare(Blue))
}

// Conc is registered entirely from concurrent goroutines to exercise the
// read-and-register atomicity of NextInt under the race detector.
type Conc struct{ enum.IntEnum[Conc] }

func TestNextIntConcurrent(t *testing.T) {
	const n = 200
	before := len(enum.Values[Conc]()) // idempotent across -count reruns
	var wg sync.WaitGroup
	got := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i] = enum.NextInt[Conc]().Int()
		}(i)
	}
	wg.Wait()

	seen := make(map[int]bool, n)
	for _, v := range got {
		require.Falsef(t, seen[v], "NextInt handed out duplicate value %d", v)
		seen[v] = true
	}
	require.Equal(t, n, len(enum.Values[Conc]())-before)
}

// Zero-value detection: members backed by "" / 0 must still differ from the
// Go zero value.
type EmptyStr struct{ enum.StringEnum[EmptyStr] }
type ZeroInt struct{ enum.IntEnum[ZeroInt] }

var (
	Blank  = enum.New[EmptyStr]("") // empty-string member
	Naught = enum.New[ZeroInt](0)   // zero-int member
)

func TestZeroValueDistinct(t *testing.T) {
	// A member backed by "" / 0 stays distinct from the Go zero value, so the
	// zero value works as an "unset" sentinel detectable with == Type{}.
	require.NotEqual(t, EmptyStr{}, Blank)
	require.NotEqual(t, ZeroInt{}, Naught)
	require.False(t, (EmptyStr{}).IsValid())
	require.True(t, Blank.IsValid())
	require.Equal(t, "", Blank.String())

	// A member survives a JSON round-trip equal to the registered one — not a
	// zero value that merely shares the payload.
	b, err := json.Marshal(Naught)
	require.NoError(t, err)
	var got ZeroInt
	require.NoError(t, json.Unmarshal(b, &got))
	require.Equal(t, Naught, got)
	require.NotEqual(t, ZeroInt{}, got)
}

func TestZeroValueMarshalGuard(t *testing.T) {
	var zs EmptyStr
	require.Equal(t, "<invalid EmptyStr>", zs.String())
	_, err := zs.MarshalText()
	var zsErr *enum.ZeroMarshalError[EmptyStr]
	require.ErrorAs(t, err, &zsErr)
	_, err = json.Marshal(zs)
	require.Error(t, err)

	var zi ZeroInt
	require.Equal(t, "<invalid ZeroInt>", zi.String())
	_, err = zi.MarshalJSON()
	var ziErr *enum.ZeroMarshalError[ZeroInt]
	require.ErrorAs(t, err, &ziErr)
	_, err = json.Marshal(zi)
	require.Error(t, err)

	// A member backed by 0 is present, so it still marshals fine.
	_, err = json.Marshal(Naught)
	require.NoError(t, err)
}

type Dup struct{ enum.StringEnum[Dup] }

func TestDuplicateRegistrationPanics(t *testing.T) {
	// Both New calls live inside the assertion so the test is also safe under
	// -count>1, where the very first call is itself the duplicate (Dup persists
	// in the registry).
	require.Panics(t, func() {
		_ = enum.New[Dup]("x")
		_ = enum.New[Dup]("x") // same value again -> panic
	})
}

func TestIntLookup(t *testing.T) {
	got, ok := enum.Lookup[Color](1)
	require.True(t, ok)
	require.Equal(t, Green, got)

	_, ok = enum.Lookup[Color](99)
	require.False(t, ok, "unknown int must miss")

	require.True(t, Blue.IsValid())
	require.True(t, enum.Valid[Color](2))
	require.False(t, enum.Valid[Color](99))
}

func TestIntJSONIsNumber(t *testing.T) {
	b, err := json.Marshal(Green)
	require.NoError(t, err)
	require.Equal(t, "1", string(b), "int enum should marshal as a bare number")

	var c Color
	require.NoError(t, json.Unmarshal([]byte(`2`), &c))
	require.Equal(t, Blue, c)

	err = json.Unmarshal([]byte(`99`), &c)
	var invalid *enum.InvalidValueError[Color]
	require.ErrorAs(t, err, &invalid)
}

func TestIntInStruct(t *testing.T) {
	type payload struct {
		Suit Suit  `json:"suit"`
		Hue  Color `json:"hue"`
	}
	in := payload{Suit: Spades, Hue: Blue}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	require.JSONEq(t, `{"suit":"spades","hue":2}`, string(b))

	var out payload
	require.NoError(t, json.Unmarshal(b, &out))
	require.Equal(t, in, out)
}

// TestUnsetFieldSerialization pins the README's claim: an unset (zero) enum
// field is a marshal error by default, but json:",omitzero" or a pointer make
// "unset" serializable.
func TestUnsetFieldSerialization(t *testing.T) {
	// Default: an unset enum field surfaces as a marshal error.
	type plain struct {
		Hue Color `json:"hue"`
	}
	_, err := json.Marshal(plain{}) // Hue is the zero value
	var zme *enum.ZeroMarshalError[Color]
	require.ErrorAs(t, err, &zme, "unset field should error without a workaround")

	// omitzero: the zero field is dropped; a real member still marshals.
	type withOmitzero struct {
		Hue Color `json:"hue,omitzero"`
	}
	b, err := json.Marshal(withOmitzero{})
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(b))

	b, err = json.Marshal(withOmitzero{Hue: Blue})
	require.NoError(t, err)
	require.JSONEq(t, `{"hue":2}`, string(b))

	// pointer: nil serializes as null; a real member still marshals.
	type withPtr struct {
		Hue *Color `json:"hue"`
	}
	b, err = json.Marshal(withPtr{})
	require.NoError(t, err)
	require.JSONEq(t, `{"hue":null}`, string(b))

	blue := Blue
	b, err = json.Marshal(withPtr{Hue: &blue})
	require.NoError(t, err)
	require.JSONEq(t, `{"hue":2}`, string(b))

	// A non-nil pointer to the zero value is not "unset" — it still errors,
	// since it points at an invalid member. Only nil means unset.
	_, err = json.Marshal(withPtr{Hue: &Color{}})
	require.ErrorAs(t, err, &zme)

	// Unmarshal, the other direction: an absent omitzero field stays the zero
	// value; an absent or null pointer field stays nil; present values decode.
	var oz withOmitzero
	require.NoError(t, json.Unmarshal([]byte(`{}`), &oz))
	require.Equal(t, withOmitzero{}, oz)
	require.NoError(t, json.Unmarshal([]byte(`{"hue":2}`), &oz))
	require.Equal(t, Blue, oz.Hue)

	var ptrMissing withPtr
	require.NoError(t, json.Unmarshal([]byte(`{}`), &ptrMissing))
	require.Nil(t, ptrMissing.Hue)

	var ptrNull withPtr
	require.NoError(t, json.Unmarshal([]byte(`{"hue":null}`), &ptrNull))
	require.Nil(t, ptrNull.Hue)

	var ptrSet withPtr
	require.NoError(t, json.Unmarshal([]byte(`{"hue":2}`), &ptrSet))
	require.NotNil(t, ptrSet.Hue)
	require.Equal(t, Blue, *ptrSet.Hue)
}

func TestSQL(t *testing.T) {
	// Valuer: StringEnum -> string, IntEnum -> int64.
	v, err := Hearts.Value()
	require.NoError(t, err)
	require.Equal(t, "hearts", v)

	v, err = Blue.Value()
	require.NoError(t, err)
	require.Equal(t, int64(2), v)

	// Scanner: text column (string or []byte) -> member.
	var s Suit
	require.NoError(t, s.Scan("spades"))
	require.Equal(t, Spades, s)
	require.NoError(t, s.Scan([]byte("hearts")))
	require.Equal(t, Hearts, s)

	// Scanner: integer column -> member.
	var c Color
	require.NoError(t, c.Scan(int64(1)))
	require.Equal(t, Green, c)

	// NULL leaves the zero value (use a *T pointer for a nullable column).
	var ns Suit
	require.NoError(t, ns.Scan(nil))
	require.Equal(t, Suit{}, ns)
	var nc Color
	require.NoError(t, nc.Scan(nil))
	require.Equal(t, Color{}, nc)

	// The zero value is not persistable.
	_, err = (Suit{}).Value()
	var zse *enum.ZeroMarshalError[Suit]
	require.ErrorAs(t, err, &zse)
	_, err = (Color{}).Value()
	var zce *enum.ZeroMarshalError[Color]
	require.ErrorAs(t, err, &zce)

	// Unknown stored value -> *InvalidValueError.
	var bs Suit
	var ive *enum.InvalidValueError[Suit]
	require.ErrorAs(t, bs.Scan("bogus"), &ive)
	var bc Color
	var ivc *enum.InvalidValueError[Color]
	require.ErrorAs(t, bc.Scan(int64(99)), &ivc)

	// A column of the wrong kind is a plain error, not InvalidValueError.
	var wrong Suit
	err = wrong.Scan(int64(1))
	require.Error(t, err)
	require.NotErrorAs(t, err, &ive)
}

func TestUnmarshalJSONNullIsNoOp(t *testing.T) {
	// IntEnum: null must NOT decode to the value-0 member; it leaves the field.
	type intHolder struct {
		Hue Color `json:"hue"`
	}
	h := intHolder{Hue: Blue}
	require.NoError(t, json.Unmarshal([]byte(`{"hue":null}`), &h))
	require.Equal(t, Blue, h.Hue, "null should be a no-op, not decode to value 0")

	var fresh intHolder
	require.NoError(t, json.Unmarshal([]byte(`{"hue":null}`), &fresh))
	require.Equal(t, Color{}, fresh.Hue)

	// StringEnum: null is likewise a no-op (the text path never fires on null).
	type strHolder struct {
		Suit Suit `json:"suit"`
	}
	sh := strHolder{Suit: Spades}
	require.NoError(t, json.Unmarshal([]byte(`{"suit":null}`), &sh))
	require.Equal(t, Spades, sh.Suit)

	// Defensive: a JSON encoder that hands UnmarshalJSON padded bytes (stdlib
	// trims, but the Unmarshaler contract doesn't require it) is still a no-op.
	c := Blue
	require.NoError(t, c.UnmarshalJSON([]byte("  null  ")))
	require.Equal(t, Blue, c)
}

func TestParse(t *testing.T) {
	got, err := enum.Parse[Suit]("hearts")
	require.NoError(t, err)
	require.Equal(t, Hearts, got)

	got, err = enum.Parse[Suit]("nope")
	require.Equal(t, Suit{}, got)
	var ive *enum.InvalidValueError[Suit]
	require.ErrorAs(t, err, &ive)
	require.Equal(t, "nope", ive.Value)

	c, err := enum.Parse[Color](1)
	require.NoError(t, err)
	require.Equal(t, Green, c)

	_, err = enum.Parse[Color](99)
	var ivc *enum.InvalidValueError[Color]
	require.ErrorAs(t, err, &ivc)
	require.Equal(t, "99", ivc.Value)
}

func TestMustParse(t *testing.T) {
	require.Equal(t, Spades, enum.MustParse[Suit]("spades"))
	require.Equal(t, Green, enum.MustParse[Color](1))

	require.Panics(t, func() { _ = enum.MustParse[Suit]("nope") })

	// the panic value is the typed *InvalidValueError.
	var ive *enum.InvalidValueError[Color]
	func() {
		defer func() {
			err, _ := recover().(error)
			require.ErrorAs(t, err, &ive)
		}()
		_ = enum.MustParse[Color](99)
	}()
	require.Equal(t, "99", ive.Value)
}

// --- tags ---------------------------------------------------------------

// CardTag is one flat tag namespace (groups + tiers), so they cross-query.
type CardTag struct{ enum.StringEnum[CardTag] }

var (
	GroupA = enum.New[CardTag]("group-a")
	GroupB = enum.New[CardTag]("group-b")
	Tier1  = enum.New[CardTag]("tier-1")
	Tier2  = enum.New[CardTag]("tier-2")
)

// Rarity is a second, unrelated tag type to exercise heterogeneous tags.
type Rarity int

const (
	Common Rarity = 1
	Rare   Rarity = 2
)

type Card struct{ enum.StringEnum[Card] }

var (
	CA1 = enum.New[Card]("a.1", enum.Tag(GroupA), enum.Tag(Tier1), enum.Tag(Common))
	CA2 = enum.New[Card]("a.2", enum.Tag(GroupA), enum.Tag(Tier2), enum.Tag(Rare))
	CB1 = enum.New[Card]("b.1", enum.Tag(GroupB), enum.Tag(Tier1), enum.Tag(Rare))
	CB2 = enum.New[Card]("b.2", enum.Tag(GroupB), enum.Tag(Tier2), enum.Tag(Common))
)

func TestTags(t *testing.T) {
	// ValuesWithTag: single tag, in registration order.
	require.Equal(t, []Card{CA1, CA2}, enum.ValuesWithTag[Card](GroupA))
	require.Equal(t, []Card{CA1, CB1}, enum.ValuesWithTag[Card](Tier1))

	// ValuesWithAnyTags: union, deduped, in registration order.
	require.Equal(t, []Card{CA1, CA2, CB1}, enum.ValuesWithAnyTags[Card](GroupA, Tier1)) // motivating example
	require.Equal(t, []Card{CA1, CA2, CB1, CB2}, enum.ValuesWithAnyTags[Card](GroupA, GroupB))

	// ValuesWithAllTags: intersection.
	require.Equal(t, []Card{CA1}, enum.ValuesWithAllTags[Card](GroupA, Tier1))
	require.Empty(t, enum.ValuesWithAllTags[Card](GroupA, GroupB)) // nothing is in both groups

	// A second, unrelated tag type, queried separately.
	require.Equal(t, []Card{CA1, CB2}, enum.ValuesWithTag[Card](Common))

	// Tag types may be mixed in one query.
	require.Equal(t, []Card{CA1, CA2, CB2}, enum.ValuesWithAnyTags[Card](GroupA, Common)) // GroupA OR Common
	require.Equal(t, []Card{CA1}, enum.ValuesWithAllTags[Card](GroupA, Common))           // GroupA AND Common

	// Empty union query -> nil.
	require.Nil(t, enum.ValuesWithAnyTags[Card]())

	// HasTag method (any arg).
	require.True(t, CA1.HasTag(GroupA))
	require.True(t, CA1.HasTag(Tier1))
	require.True(t, CA1.HasTag(Common))
	require.False(t, CA1.HasTag(GroupB))
	require.False(t, CA1.HasTag(Rare))
	require.False(t, CA1.HasTag("group-a")) // wrong type (string != CardTag) -> false

	// Tags() reverse lookup, and the zero value has none.
	require.ElementsMatch(t, []any{GroupA, Tier1, Common}, CA1.Tags())
	var z Card
	require.False(t, z.HasTag(GroupA))
	require.Empty(t, z.Tags())

	// Untagged enums (Suit) have no tags and miss every query.
	require.Empty(t, enum.ValuesWithTag[Suit](GroupA))
	require.False(t, Hearts.HasTag(GroupA))
}

// --- casting between parallel enums --------------------------------------

// ApiSuit mirrors Suit exactly — the "same enum, different codegen" scenario.
type ApiSuit struct{ enum.StringEnum[ApiSuit] }

var (
	ApiHearts   = enum.New[ApiSuit]("hearts")
	ApiDiamonds = enum.New[ApiSuit]("diamonds")
	ApiSpades   = enum.New[ApiSuit]("spades")
)

// PartialSuit is missing "spades", so casts of Spades into it miss.
type PartialSuit struct{ enum.StringEnum[PartialSuit] }

var (
	PartialHearts   = enum.New[PartialSuit]("hearts")
	PartialDiamonds = enum.New[PartialSuit]("diamonds")
)

// ApiColor mirrors Color (0, 1, 2) for the int-backed cast path.
type ApiColor struct{ enum.IntEnum[ApiColor] }

var (
	ApiRed   = enum.NextInt[ApiColor]() // 0
	ApiGreen = enum.NextInt[ApiColor]() // 1
	ApiBlue  = enum.NextInt[ApiColor]() // 2
)

func TestLookupAs(t *testing.T) {
	got, ok := enum.LookupAs[ApiSuit](Diamonds)
	require.True(t, ok)
	require.Equal(t, ApiDiamonds, got)
	require.Equal(t, 1, got.Index(), "Index must come from the target registry")

	// Round-trip back to the source type.
	back, ok := enum.LookupAs[Suit](got)
	require.True(t, ok)
	require.Equal(t, Diamonds, back)

	// Int-backed enums cast by int value.
	c, ok := enum.LookupAs[ApiColor](Green)
	require.True(t, ok)
	require.Equal(t, ApiGreen, c)

	// A value missing in the target misses.
	_, ok = enum.LookupAs[PartialSuit](Spades)
	require.False(t, ok)

	// The zero value casts to the zero value ("unset" travels).
	z, ok := enum.LookupAs[ApiSuit](Suit{})
	require.True(t, ok)
	require.True(t, z.IsZero())
}

func TestAs(t *testing.T) {
	got, err := enum.As[ApiSuit](Hearts)
	require.NoError(t, err)
	require.Equal(t, ApiHearts, got)

	c, err := enum.As[ApiColor](Blue)
	require.NoError(t, err)
	require.Equal(t, ApiBlue, c)

	z, err := enum.As[ApiSuit](Suit{})
	require.NoError(t, err)
	require.True(t, z.IsZero())

	// A miss is an *InvalidValueError[To] carrying the offending value.
	_, err = enum.As[PartialSuit](Spades)
	var ive *enum.InvalidValueError[PartialSuit]
	require.ErrorAs(t, err, &ive)
	require.Equal(t, "spades", ive.Value)
}

func TestMustAs(t *testing.T) {
	require.Equal(t, ApiSpades, enum.MustAs[ApiSuit](Spades))
	require.Equal(t, ApiRed, enum.MustAs[ApiColor](Red))
	require.True(t, enum.MustAs[ApiSuit](Suit{}).IsZero())

	require.Panics(t, func() { _ = enum.MustAs[PartialSuit](Spades) })

	// The panic value is the typed *InvalidValueError[To].
	var ive *enum.InvalidValueError[PartialSuit]
	func() {
		defer func() {
			err, _ := recover().(error)
			require.ErrorAs(t, err, &ive)
		}()
		_ = enum.MustAs[PartialSuit](Spades)
	}()
	require.Equal(t, "spades", ive.Value)
}

func TestSameValues(t *testing.T) {
	// Exactly equal sets, both kinds.
	require.NoError(t, enum.SameValues[Suit, ApiSuit]())
	require.NoError(t, enum.SameValues[Color, ApiColor]())
	require.NoError(t, enum.SameValues[Suit, Suit]())

	// Subset: the missing value is reported on the right side.
	err := enum.SameValues[Suit, PartialSuit]()
	require.Error(t, err)
	require.Contains(t, err.Error(), `"spades"`)
	require.Contains(t, err.Error(), "only in enum_test.Suit")

	// Symmetric: also reported when the larger set is on the right.
	err = enum.SameValues[PartialSuit, Suit]()
	require.Error(t, err)
	require.Contains(t, err.Error(), "only in enum_test.Suit")

	// Disjoint kinds are an error, not "different sets".
	err = enum.SameValues[Suit, Color]()
	require.Error(t, err)
	require.Contains(t, err.Error(), "string-backed")
	require.Contains(t, err.Error(), "int-backed")
}
