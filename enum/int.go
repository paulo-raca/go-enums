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
// *InvalidValueError[T].
type IntEnum[T Enum] struct {
	val int
	// pos is the 1-based registration order; 0 only for the Go zero value. It
	// both orders members and marks the zero value invalid, so IntEnum{} stays
	// distinct from New[T](0). See StringEnum.pos.
	pos int
}

// String returns the decimal form of the backing value, or "<invalid>" for the
// zero value. Implements fmt.Stringer. The check is the lock-free pos field.
func (e IntEnum[T]) String() string {
	if e.pos == 0 {
		return invalidString
	}
	return strconv.Itoa(e.val)
}

// Value returns the backing integer.
func (e IntEnum[T]) Value() int { return e.val }

// IsValid reports whether e is a real member rather than the Go zero value. It
// is the lock-free pos field, so it is cheap; it does not consult the registry.
// Mirrors reflect.Value.IsValid.
func (e IntEnum[T]) IsValid() bool { return e.pos != 0 }

// Position returns the member's 1-based registration order, or 0 for the zero
// value. See StringEnum.Position.
func (e IntEnum[T]) Position() int { return e.pos }

// MarshalText implements encoding.TextMarshaler, emitting the decimal value.
// This is what encoding/json uses for an IntEnum used as a map key. Encoding the
// zero value yields *ZeroMarshalError[T].
func (e IntEnum[T]) MarshalText() ([]byte, error) {
	if e.pos == 0 {
		return nil, &ZeroMarshalError[T]{}
	}
	return []byte(strconv.Itoa(e.val)), nil
}

// MarshalJSON implements json.Marshaler, emitting a bare JSON number.
// encoding/json prefers this over MarshalText for ordinary values. Encoding the
// zero value yields *ZeroMarshalError[T].
func (e IntEnum[T]) MarshalJSON() ([]byte, error) {
	if e.pos == 0 {
		return nil, &ZeroMarshalError[T]{}
	}
	return []byte(strconv.Itoa(e.val)), nil
}

// set is the unexported value write path; see StringEnum.set. It shares the name
// with StringEnum's set(string) so a single New serves both.
func (e *IntEnum[T]) set(n int) { e.val = n }

// setPos records the 1-based registration position; see StringEnum.setPos.
func (e *IntEnum[T]) setPos(p int) { e.pos = p }

// isEnumMember is the unexported marker required by the Enum constraint; see
// StringEnum.isEnumMember.
func (IntEnum[T]) isEnumMember() {}

// UnmarshalText implements encoding.TextUnmarshaler. It parses a decimal value,
// checks membership in T, and stores the value and position, or returns
// *InvalidValueError[T].
func (e *IntEnum[T]) UnmarshalText(text []byte) error {
	n, err := strconv.Atoi(string(text))
	if err != nil {
		return &InvalidValueError[T]{Value: string(text)}
	}
	v, ok := lookup[T](n)
	if !ok {
		return &InvalidValueError[T]{Value: string(text)}
	}
	e.set(n)
	e.setPos(v.Position())
	return nil
}

// UnmarshalJSON implements json.Unmarshaler. It parses a JSON number, checks
// membership in T, and stores the value and position, or returns
// *InvalidValueError[T].
func (e *IntEnum[T]) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return &InvalidValueError[T]{Value: string(data)}
	}
	v, ok := lookup[T](n)
	if !ok {
		return &InvalidValueError[T]{Value: strconv.Itoa(n)}
	}
	e.set(n)
	e.setPos(v.Position())
	return nil
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
// Mix freely with explicit New values to start or jump the sequence. NextInt is
// intended for init-time var blocks, just like iota, but is also safe to call
// concurrently: it computes the next value and registers it under a single
// write lock, so two callers can never be handed the same value.
func NextInt[T Enum, PT interface {
	*T
	set(int)
	setPos(int)
}]() T {
	mu.Lock()
	defer mu.Unlock()
	n := nextIntLocked[T]()
	var t T
	PT(&t).set(n)
	registerLocked[T, PT](&t, true, n)
	return t
}
