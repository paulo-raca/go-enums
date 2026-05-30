package b

import (
	"a"

	"github.com/paulo-raca/go-enums/enum"
)

// Cross-package switch: members come from a's exported facts.
func describe(s a.Suit) string {
	switch s { // want `non-exhaustive switch on a\.Suit: missing Spades`
	case a.Hearts:
		return "h"
	case a.Diamonds:
		return "d"
	}
	return ""
}

func exhaustive(s a.Suit) string {
	switch s {
	case a.Hearts, a.Diamonds, a.Spades:
		return "x"
	}
	return ""
}

// Members must live in the enum type's own package, not here.
var Rogue = enum.New[a.Suit]("rogue") // want `enum members of a\.Suit must be declared in its own package`
