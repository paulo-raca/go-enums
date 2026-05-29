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
	// present is false only for the Go zero value; set() marks every member it
	// builds. It is what makes IntEnum{} distinct from New[T](0). See
	// StringEnum.present.
	present bool
}

// String returns the decimal form of the backing value, or "<invalid>" for the
// zero value. Implements fmt.Stringer. The check is the lock-free present flag.
func (e IntEnum[T]) String() string {
	if !e.present {
		return invalidString
	}
	return strconv.Itoa(e.val)
}

// Value returns the backing integer.
func (e IntEnum[T]) Value() int { return e.val }

// MarshalText implements encoding.TextMarshaler, emitting the decimal value.
// This is what encoding/json uses for an IntEnum used as a map key. Encoding the
// zero value is refused; see zeroMarshalErr.
func (e IntEnum[T]) MarshalText() ([]byte, error) {
	if !e.present {
		return nil, zeroMarshalErr[T]()
	}
	return []byte(strconv.Itoa(e.val)), nil
}

// MarshalJSON implements json.Marshaler, emitting a bare JSON number.
// encoding/json prefers this over MarshalText for ordinary values. Encoding the
// zero value is refused; see zeroMarshalErr.
func (e IntEnum[T]) MarshalJSON() ([]byte, error) {
	if !e.present {
		return nil, zeroMarshalErr[T]()
	}
	return []byte(strconv.Itoa(e.val)), nil
}

// set is the unexported write path that closes the set; see StringEnum.set.
// It shares the name with StringEnum's set(string) so a single New serves both.
func (e *IntEnum[T]) set(n int) { e.val, e.present = n, true }

// isEnumMember is the unexported marker required by the Enum constraint; see
// StringEnum.isEnumMember.
func (IntEnum[T]) isEnumMember() {}

// UnmarshalText implements encoding.TextUnmarshaler. It parses a decimal value,
// checks membership in T, and stores it, or returns *InvalidValueError[T].
func (e *IntEnum[T]) UnmarshalText(text []byte) error {
	n, err := strconv.Atoi(string(text))
	if err != nil {
		return &InvalidValueError[T]{Value: string(text)}
	}
	if _, ok := lookup[T](n); !ok {
		return &InvalidValueError[T]{Value: string(text)}
	}
	e.set(n)
	return nil
}

// UnmarshalJSON implements json.Unmarshaler. It parses a JSON number, checks
// membership in T, and stores it, or returns *InvalidValueError[T].
func (e *IntEnum[T]) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return &InvalidValueError[T]{Value: string(data)}
	}
	if _, ok := lookup[T](n); !ok {
		return &InvalidValueError[T]{Value: strconv.Itoa(n)}
	}
	e.set(n)
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
}]() T {
	mu.Lock()
	defer mu.Unlock()
	n := nextIntLocked[T]()
	var t T
	PT(&t).set(n)
	registerLocked[T](t, true, n)
	return t
}
