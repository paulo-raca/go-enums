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

	got, ok := enum.FromValue[Suit]("spades")
	require.True(t, ok)
	require.Equal(t, Spades, got)

	_, ok = enum.FromValue[Suit]("nope")
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

func TestPosition(t *testing.T) {
	// 0-based registration order; zero value is -1.
	require.Equal(t, 0, Hearts.Position())
	require.Equal(t, 1, Diamonds.Position())
	require.Equal(t, 2, Spades.Position())

	var z Suit
	require.Equal(t, -1, z.Position())

	// Values is in registration order, and Position matches the index.
	for i, v := range enum.Values[Suit]() {
		require.Equalf(t, i, v.Position(), "Values[%d].Position()", i)
	}

	// Position survives a JSON round-trip (it is copied from the canonical member).
	b, err := json.Marshal(Diamonds)
	require.NoError(t, err)
	var got Suit
	require.NoError(t, json.Unmarshal(b, &got))
	require.Equal(t, 1, got.Position())
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
	got, ok := enum.FromValue[Color](1)
	require.True(t, ok)
	require.Equal(t, Green, got)

	_, ok = enum.FromValue[Color](99)
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
