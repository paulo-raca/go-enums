package enum_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/paulo-raca/go-enums/enum"
)

// --- string enum under test ---------------------------------------------

type AIStopReason struct{ enum.StringEnum[AIStopReason] }

var (
	AIStopReasonEndTurn   = enum.NewString[AIStopReason]("end_turn")
	AIStopReasonMaxTokens = enum.NewString[AIStopReason]("max_tokens")
	AIStopReasonToolUse   = enum.NewString[AIStopReason]("tool_use")
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
	Low  = enum.NewInt[Level](10) // explicit start
	Mid  = enum.NextInt[Level]()  // 11
	High = enum.NextInt[Level]()  // 12
)

func TestStringBasics(t *testing.T) {
	if AIStopReasonEndTurn.String() != "end_turn" {
		t.Fatalf("String = %q", AIStopReasonEndTurn.String())
	}
	if !enum.Valid(AIStopReasonEndTurn) {
		t.Fatal("registered member should be valid")
	}
	var zero AIStopReason
	if enum.Valid(zero) {
		t.Fatal("zero value must not be valid")
	}
	got, ok := enum.FromString[AIStopReason]("tool_use")
	if !ok || got != AIStopReasonToolUse {
		t.Fatalf("FromString = %v, %v", got, ok)
	}
	if _, ok := enum.FromString[AIStopReason]("nope"); ok {
		t.Fatal("unknown string must miss")
	}
}

func TestStringValuesOrder(t *testing.T) {
	want := []AIStopReason{AIStopReasonEndTurn, AIStopReasonMaxTokens, AIStopReasonToolUse}
	if got := enum.Values[AIStopReason](); !reflect.DeepEqual(got, want) {
		t.Fatalf("Values = %v, want %v", got, want)
	}
}

func TestStringJSON(t *testing.T) {
	b, err := json.Marshal(AIStopReasonMaxTokens)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"max_tokens"` {
		t.Fatalf("marshal = %s", b)
	}
	var r AIStopReason
	if err := json.Unmarshal([]byte(`"end_turn"`), &r); err != nil {
		t.Fatal(err)
	}
	if r != AIStopReasonEndTurn {
		t.Fatalf("unmarshal = %v", r)
	}

	err = json.Unmarshal([]byte(`"bogus"`), &r)
	var invalid *enum.InvalidError[AIStopReason]
	if !errors.As(err, &invalid) || invalid.Value != "bogus" {
		t.Fatalf("want *InvalidError, got %v", err)
	}
}

func TestStringAsJSONMapKey(t *testing.T) {
	m := map[AIStopReason]int{AIStopReasonToolUse: 3}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"tool_use":3}` {
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

func TestIntLookup(t *testing.T) {
	if got, ok := enum.FromInt[Color](1); !ok || got != Green {
		t.Fatalf("FromInt(1) = %v, %v", got, ok)
	}
	if _, ok := enum.FromInt[Color](99); ok {
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
	var invalid *enum.InvalidError[Color]
	if !errors.As(err, &invalid) {
		t.Fatalf("want *InvalidError, got %v", err)
	}
}

func TestIntInStruct(t *testing.T) {
	type payload struct {
		Stop AIStopReason `json:"stop"`
		Hue  Color        `json:"hue"`
	}
	in := payload{Stop: AIStopReasonToolUse, Hue: Blue}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"stop":"tool_use","hue":2}` {
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
