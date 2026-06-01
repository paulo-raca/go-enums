package enum

import (
	"bytes"
	"cmp"
	"database/sql/driver"
	"encoding/json"
	"fmt"
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
	// index is the 1-based registration order; 0 only for the Go zero value. It
	// both orders members and marks the zero value invalid, so IntEnum{} stays
	// distinct from New[T](0). See StringEnum.index.
	index int
}

// String returns the decimal form of the backing value, or "<invalid T>" for
// the zero value. Implements fmt.Stringer. The check is the lock-free index field.
func (e IntEnum[T]) String() string {
	if e.index == 0 {
		return invalidString[T]()
	}
	return strconv.Itoa(e.val)
}

// Int returns the backing integer.
func (e IntEnum[T]) Int() int { return e.val }

// IsValid reports whether e is a real member rather than the Go zero value. It
// is the lock-free index field, so it is cheap; it does not consult the registry.
// Mirrors reflect.Value.IsValid.
func (e IntEnum[T]) IsValid() bool { return e.index != 0 }

// IsZero reports whether e is the Go zero value (the inverse of IsValid); see
// StringEnum.IsZero.
func (e IntEnum[T]) IsZero() bool { return e.index == 0 }

// Index returns the member's 0-based registration order, or -1 for the zero
// value. See StringEnum.Index.
func (e IntEnum[T]) Index() int { return e.index - 1 }

// Compare orders members by registration position (NOT by their int value),
// returning -1, 0, or +1. See StringEnum.Compare.
func (e IntEnum[T]) Compare(other T) int { return cmp.Compare(e.Index(), other.Index()) }

// MarshalText implements encoding.TextMarshaler, emitting the decimal value.
// This is what encoding/json uses for an IntEnum used as a map key. Encoding the
// zero value yields *ZeroMarshalError[T].
func (e IntEnum[T]) MarshalText() ([]byte, error) {
	if e.index == 0 {
		return nil, &ZeroMarshalError[T]{}
	}
	return []byte(strconv.Itoa(e.val)), nil
}

// MarshalJSON implements json.Marshaler, emitting a bare JSON number.
// encoding/json prefers this over MarshalText for ordinary values. Encoding the
// zero value yields *ZeroMarshalError[T].
func (e IntEnum[T]) MarshalJSON() ([]byte, error) {
	if e.index == 0 {
		return nil, &ZeroMarshalError[T]{}
	}
	return []byte(strconv.Itoa(e.val)), nil
}

// Value implements driver.Valuer: an IntEnum is stored as an int64. The zero
// value is refused with *ZeroMarshalError[T] — an invalid value must not be
// persisted. For a nullable column use a *T pointer.
func (e IntEnum[T]) Value() (driver.Value, error) {
	if e.index == 0 {
		return nil, &ZeroMarshalError[T]{}
	}
	return int64(e.val), nil
}

// set is the unexported value write path; see StringEnum.set. It shares the name
// with StringEnum's set(string) so a single New serves both.
func (e *IntEnum[T]) set(n int) { e.val = n }

// setIndex records the 1-based registration position; see StringEnum.setIndex.
func (e *IntEnum[T]) setIndex(p int) { e.index = p }

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
	v, ok := resolve[T](n)
	if !ok {
		return &InvalidValueError[T]{Value: string(text)}
	}
	e.set(n)
	e.setIndex(v.Index() + 1) // Index is 0-based; setIndex wants the 1-based slot
	return nil
}

// UnmarshalJSON implements json.Unmarshaler. It parses a JSON number, checks
// membership in T, and stores the value and position, or returns
// *InvalidValueError[T]. JSON null is a no-op (the member is left unchanged), by
// the convention for json.Unmarshaler — matching StringEnum, whose text path
// also ignores null.
func (e *IntEnum[T]) UnmarshalJSON(data []byte) error {
	// encoding/json hands us whitespace-trimmed bytes, but trim defensively so
	// alternative JSON encoders that don't are still handled. (Go optimizes
	// string(b) == "literal" to avoid allocating.)
	if string(bytes.TrimSpace(data)) == "null" {
		return nil
	}
	var n int
	if err := json.Unmarshal(data, &n); err != nil {
		return &InvalidValueError[T]{Value: string(data)}
	}
	v, ok := resolve[T](n)
	if !ok {
		return &InvalidValueError[T]{Value: strconv.Itoa(n)}
	}
	e.set(n)
	e.setIndex(v.Index() + 1) // Index is 0-based; setIndex wants the 1-based slot
	return nil
}

// Scan implements sql.Scanner from an integer column. A NULL (nil src) leaves
// the zero value; an unknown value yields *InvalidValueError[T].
func (e *IntEnum[T]) Scan(src any) error {
	if src == nil {
		return nil
	}
	var n int
	switch v := src.(type) {
	case int64:
		n = int(v)
	case int:
		n = v
	default:
		var zero T
		return fmt.Errorf("enum: cannot scan %T into %T", src, zero)
	}
	m, ok := resolve[T](n)
	if !ok {
		return &InvalidValueError[T]{Value: strconv.Itoa(n)}
	}
	e.set(n)
	e.setIndex(m.Index() + 1)
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
// write lock, so two callers can never be handed the same value. Optional Tag
// options attach tags to the member; see Tag.
func NextInt[T Enum, PT interface {
	*T
	set(int)
	setIndex(int)
}](opts ...Option) T {
	tags := tagsOf(opts)
	mu.Lock()
	defer mu.Unlock()
	n := nextIntLocked[T]()
	var t T
	PT(&t).set(n)
	registerLocked[T, PT](&t, true, n, tags)
	return t
}

// HasTag reports whether e was tagged (via enum.Tag) with tag. tag is any value
// — a tag of the wrong type simply returns false. The zero value has no tags.
func (e IntEnum[T]) HasTag(tag any) bool { return hasTag[T](e.index, tag) }

// Tags returns e's tags in declaration order. The slice is heterogeneous (a
// member may be tagged with several types), so it is []any.
func (e IntEnum[T]) Tags() []any { return memberTags[T](e.index) }
