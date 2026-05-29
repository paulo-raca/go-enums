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
