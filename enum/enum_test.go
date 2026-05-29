package enum_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/paulo-raca/go-enums/enum"
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
	if Hearts.String() != "hearts" {
		t.Fatalf("String = %q", Hearts.String())
	}
	if !enum.Valid(Hearts) {
		t.Fatal("registered member should be valid")
	}
	var zero Suit
	if enum.Valid(zero) {
		t.Fatal("zero value must not be valid")
	}
	got, ok := enum.FromValue[Suit]("spades")
	if !ok || got != Spades {
		t.Fatalf("FromValue = %v, %v", got, ok)
	}
	if _, ok := enum.FromValue[Suit]("nope"); ok {
		t.Fatal("unknown string must miss")
	}
}

func TestStringValuesOrder(t *testing.T) {
	want := []Suit{Hearts, Diamonds, Spades}
	if got := enum.Values[Suit](); !reflect.DeepEqual(got, want) {
		t.Fatalf("Values = %v, want %v", got, want)
	}
}

func TestStringJSON(t *testing.T) {
	b, err := json.Marshal(Diamonds)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"diamonds"` {
		t.Fatalf("marshal = %s", b)
	}
	var r Suit
	if err := json.Unmarshal([]byte(`"hearts"`), &r); err != nil {
		t.Fatal(err)
	}
	if r != Hearts {
		t.Fatalf("unmarshal = %v", r)
	}

	err = json.Unmarshal([]byte(`"bogus"`), &r)
	var invalid *enum.InvalidValueError[Suit]
	if !errors.As(err, &invalid) || invalid.Value != "bogus" {
		t.Fatalf("want *InvalidValueError, got %v", err)
	}
}

func TestStringAsJSONMapKey(t *testing.T) {
	m := map[Suit]int{Spades: 3}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"spades":3}` {
		t.Fatalf("map marshal = %s", b)
	}
}

func TestIntAutoIncrement(t *testing.T) {
	if Red.Value() != 0 || Green.Value() != 1 || Blue.Value() != 2 {
		t.Fatalf("colors = %d,%d,%d", Red.Value(), Green.Value(), Blue.Value())
	}
	// explicit-start sequence continues from the explicit value
	if Low.Value() != 10 || Mid.Value() != 11 || High.Value() != 12 {
		t.Fatalf("levels = %d,%d,%d", Low.Value(), Mid.Value(), High.Value())
	}
}

func TestIntNextIsMaxPlusOne(t *testing.T) {
	got := []int{
		MixA.Value(), MixB.Value(), MixC.Value(),
		MixD.Value(), MixE.Value(), MixF.Value(),
	}
	want := []int{0, 100, 50, 101, -5, 102}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed values = %v, want %v", got, want)
	}
}

type Dup struct{ enum.StringEnum[Dup] }

func TestDuplicateRegistrationPanics(t *testing.T) {
	_ = enum.New[Dup]("x")
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate registration")
		}
	}()
	_ = enum.New[Dup]("x") // same value again
}

func TestIntLookup(t *testing.T) {
	if got, ok := enum.FromValue[Color](1); !ok || got != Green {
		t.Fatalf("FromValue(1) = %v, %v", got, ok)
	}
	if _, ok := enum.FromValue[Color](99); ok {
		t.Fatal("unknown int must miss")
	}
	if !enum.Valid(Blue) {
		t.Fatal("Blue should be valid")
	}
}

func TestIntJSONIsNumber(t *testing.T) {
	b, err := json.Marshal(Green)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `1` {
		t.Fatalf("int enum should marshal as number, got %s", b)
	}
	var c Color
	if err := json.Unmarshal([]byte(`2`), &c); err != nil {
		t.Fatal(err)
	}
	if c != Blue {
		t.Fatalf("unmarshal = %v", c)
	}

	err = json.Unmarshal([]byte(`99`), &c)
	var invalid *enum.InvalidValueError[Color]
	if !errors.As(err, &invalid) {
		t.Fatalf("want *InvalidValueError, got %v", err)
	}
}

func TestIntInStruct(t *testing.T) {
	type payload struct {
		Suit Suit  `json:"suit"`
		Hue  Color `json:"hue"`
	}
	in := payload{Suit: Spades, Hue: Blue}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"suit":"spades","hue":2}` {
		t.Fatalf("struct marshal = %s", b)
	}
	var out payload
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round trip = %+v, want %+v", out, in)
	}
}
