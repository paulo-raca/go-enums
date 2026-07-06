package a

import "github.com/paulo-raca/go-enums/enum"

// --- valid enum types ---

type Suit struct{ enum.StringEnum[Suit] } // want Suit:`enumMembers\(Diamonds,Hearts,Spades\)`

type Color struct{ enum.IntEnum[Color] } // want Color:`enumMembers\(Blue,Green,Red\)`

// --- Rule 1 violations ---

type BadExtra struct { // want `enum type BadExtra must embed only enum\.StringEnum\[BadExtra\] and nothing else`
	enum.StringEnum[BadExtra]
	Extra int
}

type BadSelf struct{ enum.StringEnum[Suit] } // want `enum base must be parameterised by BadSelf, not Suit`

type BadIntExtra struct { // want `enum type BadIntExtra must embed only enum\.IntEnum\[BadIntExtra\] and nothing else`
	enum.IntEnum[BadIntExtra]
	Extra int
}

type BadMixedEnum struct { // want `enum type BadMixedEnum must embed only enum\.StringEnum\[BadMixedEnum\] and nothing else`
	enum.StringEnum[BadIntExtra]
	enum.IntEnum[BadIntExtra]
	Extra int
}

// --- members ---

var (
	Hearts   = enum.New[Suit]("hearts")
	Diamonds = enum.New[Suit]("diamonds")
	Spades   = enum.New[Suit]("spades")

	Red   = enum.NextInt[Color]()
	Green = enum.NextInt[Color]()
	Blue  = enum.NextInt[Color]()
)

// Joker is a Suit-typed var that is NOT a member (no New); using it as a case
// must be rejected.
var Joker Suit

// --- Rule 2 violations ---

func makeBad() Suit {
	return enum.New[Suit]("x") // want `enum\.New must directly initialise a package-level var`
}

var Wrapped = id(enum.New[Suit]("y")) // want `enum\.New must directly initialise a package-level var`

func id(s Suit) Suit { return s }

func reassign() {
	Hearts = Spades // want `enum member Hearts must not be reassigned`
}

// --- Rule 2 (constant argument) ---

// Dyn is a small string enum used to pin the constant-arg check without
// touching Suit's fact assertions above.
type Dyn struct{ enum.StringEnum[Dyn] } // want Dyn:`enumMembers\(DynBad,DynConst,DynFolded\)`

var runtimeStr = "runtime-decided"

var (
	DynBad    = enum.New[Dyn](runtimeStr) // want `enum\.New value must be a compile-time constant`
	DynConst  = enum.New[Dyn]("ok")
	DynFolded = enum.New[Dyn]("fol" + "ded") // constant-folded literal is fine
)

// Constants (typed or not) are compile-time constants and pass the check.
const dynConstArg = "cst"

type DynFromConst struct{ enum.StringEnum[DynFromConst] } // want DynFromConst:`enumMembers\(DynFromConstOK\)`

var DynFromConstOK = enum.New[DynFromConst](dynConstArg)

// DynInt exercises the same rule on the int-backed base.
type DynInt struct{ enum.IntEnum[DynInt] } // want DynInt:`enumMembers\(DynIntBad,DynIntConst\)`

var runtimeInt = 7

var (
	DynIntBad   = enum.New[DynInt](runtimeInt) // want `enum\.New value must be a compile-time constant`
	DynIntConst = enum.New[DynInt](3)
)

// --- Rule 3: switches ---

func exhaustive(s Suit) string {
	switch s {
	case Hearts:
		return "h"
	case Diamonds:
		return "d"
	case Spades:
		return "s"
	}
	return ""
}

func grouped(s Suit) string {
	switch s {
	case Hearts, Diamonds, Spades: // one clause covering all members
		return "x"
	}
	return ""
}

func missing(s Suit) string {
	switch s { // want `non-exhaustive switch on Suit: missing Spades`
	case Hearts:
		return "h"
	case Diamonds:
		return "d"
	}
	return ""
}

func withDefault(s Suit) string {
	switch s { // default makes it exhaustive
	case Hearts:
		return "h"
	default:
		return "?"
	}
}

func nonMemberCase(s Suit) string {
	switch s {
	case Hearts, Diamonds, Spades:
		return "x"
	case Joker: // want `case in switch on Suit is not a registered member`
		return "j"
	}
	return ""
}
