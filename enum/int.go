package enum

import (
	"encoding/json"
	"strconv"
)

// IntEnum is embedded (parameterised over the embedding type) to turn a struct
// into an integer-backed enum member:
//
//	type Color struct{ enum.IntEnum[Color] }
//
// Only the integer value is persisted; there is no separate stored name.
// String() is therefore the decimal form of the value, and JSON encodes as a
// bare number. T carries the concrete identity for UnmarshalText/JSON and
// *InvalidError[T].
type IntEnum[T Enum] struct {
	val int
}

// String returns the decimal form of the backing value. Implements fmt.Stringer.
func (e IntEnum[T]) String() string { return strconv.Itoa(e.val) }

// Value returns the backing integer.
func (e IntEnum[T]) Value() int { return e.val }

// MarshalText implements encoding.TextMarshaler, emitting the decimal value.
// This is what encoding/json uses for an IntEnum used as a map key.
func (e IntEnum[T]) MarshalText() ([]byte, error) { return []byte(strconv.Itoa(e.val)), nil }

// MarshalJSON implements json.Marshaler, emitting a bare JSON number.
// encoding/json prefers this over MarshalText for ordinary values.
func (e IntEnum[T]) MarshalJSON() ([]byte, error) { return []byte(strconv.Itoa(e.val)), nil }

// setInt is the unexported write path that closes the set; see setString.
func (e *IntEnum[T]) setInt(n int) { e.val = n }

// UnmarshalText implements encoding.TextUnmarshaler. It parses a decimal value,
// checks membership in T, and stores it, or returns *InvalidError[T].
func (e *IntEnum[T]) UnmarshalText(text []byte) error {
	n, err := strconv.Atoi(string(text))
	if err != nil {
		return &InvalidError[T]{Value: string(text)}
	}
	if _, ok := FromInt[T](n); !ok {
		return &InvalidError[T]{Value: string(text)}
	}
	e.val = n
	return nil
}

// UnmarshalJSON implements json.Unmarshaler. It parses a JSON number, checks
// membership in T, and stores it, or returns *InvalidError[T].
func (e *IntEnum[T]) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return &InvalidError[T]{Value: string(data)}
	}
	if _, ok := FromInt[T](n); !ok {
		return &InvalidError[T]{Value: strconv.Itoa(n)}
	}
	e.val = n
	return nil
}

// NewInt constructs, registers, and returns an integer-backed enum member with
// an explicit value:
//
//	Low = enum.NewInt[Priority](10)
//
// The auto-increment counter is advanced past n, so a following NextInt yields
// n+1 (iota-with-explicit-start semantics). Duplicate registrations (same T,
// same value) are ignored.
func NewInt[T Enum, PT interface {
	*T
	setInt(int)
}](n int) T {
	var v T
	PT(&v).setInt(n)
	register[T](v, true, n)
	return v
}

// NextInt constructs the next member in iota-like sequence: the first call for
// a type yields 0, and each subsequent call yields one more than the highest
// value registered so far.
//
//	var (
//		Red   = enum.NextInt[Color]() // 0
//		Green = enum.NextInt[Color]() // 1
//		Blue  = enum.NextInt[Color]() // 2
//	)
//
// Mix freely with NewInt to start or jump the sequence. NextInt is intended for
// init-time var blocks, just like iota.
func NextInt[T Enum, PT interface {
	*T
	setInt(int)
}]() T {
	return NewInt[T, PT](nextInt[T]())
}
