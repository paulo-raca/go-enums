package enum

import (
	"cmp"
	"database/sql/driver"
	"fmt"
)

// StringEnum is embedded (parameterised over the embedding type) to turn a
// struct into a string-backed enum member:
//
//	type Suit struct{ enum.StringEnum[Suit] }
//
// T appears only in method signatures — it is a phantom type parameter carrying
// the concrete identity so UnmarshalText can resolve against the right member
// set and return *InvalidValueError[T]. It is never a stored field.
type StringEnum[T Enum] struct {
	val string
	// index is the 1-based registration order; 0 only for the Go zero value. It
	// both orders members and marks the zero value invalid, so StringEnum{} stays
	// distinct from New[T]("") — a member backed by the empty string is still
	// tellable from an unset field.
	index int
}

// String returns the member's canonical string, or "<invalid T>" for the zero
// value. Implements fmt.Stringer. The check is the lock-free index field, so the
// common path stays cheap (only the rare zero-value path reflects on T).
func (e StringEnum[T]) String() string {
	if e.index == 0 {
		return invalidString[T]()
	}
	return e.val
}

// IsValid reports whether e is a real member rather than the Go zero value. It
// is the lock-free index field, so it is cheap; it does not consult the registry.
// Mirrors reflect.Value.IsValid.
func (e StringEnum[T]) IsValid() bool { return e.index != 0 }

// IsZero reports whether e is the Go zero value (the inverse of IsValid).
// encoding/json's ",omitzero" option uses it to drop unset enum fields.
func (e StringEnum[T]) IsZero() bool { return e.index == 0 }

// Index returns the member's 0-based registration order, or -1 for the zero
// value. Members are ordered by registration; Values returns them in this order.
// (Internally index is 1-based so 0 marks the zero value; Index subtracts one.)
func (e StringEnum[T]) Index() int { return e.index - 1 }

// Compare orders members by registration position, returning -1, 0, or +1 as e
// sorts before, equal to, or after other. Go has no operator overloading, so
// "a < b" is spelled "a.Compare(b) < 0" (and likewise <=, >, >=); sort with
// slices.SortFunc(xs, MyEnum.Compare). The zero value (Index -1) sorts before
// every registered member.
func (e StringEnum[T]) Compare(other T) int { return cmp.Compare(e.Index(), other.Index()) }

// MarshalText implements encoding.TextMarshaler. encoding/json uses this
// automatically (quoting the result) when no MarshalJSON is present, so a
// StringEnum encodes as a JSON string and works as a JSON map key. Encoding the
// zero value yields *ZeroMarshalError[T].
func (e StringEnum[T]) MarshalText() ([]byte, error) {
	if e.index == 0 {
		return nil, &ZeroMarshalError[T]{}
	}
	return []byte(e.val), nil
}

// Value implements driver.Valuer: a StringEnum is stored as its string. The zero
// value is refused with *ZeroMarshalError[T] — an invalid value must not be
// persisted. For a nullable column use a *T pointer.
func (e StringEnum[T]) Value() (driver.Value, error) {
	if e.index == 0 {
		return nil, &ZeroMarshalError[T]{}
	}
	return e.val, nil
}

// set is the unexported value write path. Promoted onto *T it keeps this
// package's identity, which is what closes the set while still letting New
// construct values for T defined in another package. IntEnum carries a set(int)
// of the same name; that shared name is what lets a single New serve both bases.
func (e *StringEnum[T]) set(s string) { e.val = s }

// TryAs casts e to the member of To backed by the same string — for the
// parallel enums that accumulate in real projects (the sqlboiler one, the
// OpenAPI one, the business-model one), which represent the same set and should
// convert losslessly:
//
//	api, ok := sqlSuit.TryAs[OpenApiEnum]()
//
// Only To is named at the call site; PTo is inferred from *To. The set(string)
// constraint ties To to this base's backing kind, so casting a string-backed
// enum to an int-backed one is a compile error, not a runtime miss.
//
// The zero value casts to the zero value: "unset" travels across the cast (ok
// is true; IsZero holds for the result). A registered member whose value names
// no member of To yields (zero, false) — and the enumcheck analyzer flags cast
// sites whose source enum has values the target lacks (widening, where the
// source is a subset of the target, is total and is not flagged).
//
// TryAs, As, and MustAs are the cast-flavored siblings of the package-level
// TryParse, Parse, and MustParse.
func (e StringEnum[T]) TryAs[To Enum, PTo interface {
	*To
	set(string)
}]() (To, bool) {
	return castTo[To, string](e.val, e.index)
}

// As is the error-returning flavor of TryAs: a miss yields
// *InvalidValueError[To], composing with %w and errors.As like Parse.
//
//	api, err := sqlSuit.As[OpenApiEnum]()
func (e StringEnum[T]) As[To Enum, PTo interface {
	*To
	set(string)
}]() (To, error) {
	return castErr[To, string](e.val, e.index)
}

// MustAs is the panicking sibling of As — for casts between enums whose value
// sets are known to match (which the enumcheck analyzer can verify statically,
// and SameValues can assert at runtime). Panics with *InvalidValueError[To].
//
//	api := sqlSuit.MustAs[OpenApiEnum]()
func (e StringEnum[T]) MustAs[To Enum, PTo interface {
	*To
	set(string)
}]() To {
	m, err := castErr[To, string](e.val, e.index)
	if err != nil {
		panic(err)
	}
	return m
}

// setIndex records the 1-based registration position; see registerLocked. Paired
// with set in the New/NextInt constructor constraints.
func (e *StringEnum[T]) setIndex(p int) { e.index = p }

// isEnumMember is the unexported marker required by the Enum constraint. Only
// StringEnum and IntEnum define it, so embedding one of them is what makes a
// type an Enum — an arbitrary comparable Stringer cannot qualify.
func (StringEnum[T]) isEnumMember() {}

// HasTag reports whether e was tagged (via enum.Tag) with tag. tag is any value
// — a tag of the wrong type simply returns false. The zero value has no tags.
func (e StringEnum[T]) HasTag(tag any) bool { return hasTag[T](e.index, tag) }

// Tags returns e's tags in declaration order. The slice is heterogeneous (a
// member may be tagged with several types), so it is []any.
func (e StringEnum[T]) Tags() []any { return memberTags[T](e.index) }

// UnmarshalText implements encoding.TextUnmarshaler. It resolves text against
// T's registered members and copies in the canonical value and position, or
// returns *InvalidValueError[T]. JSON null is left untouched (the text path only
// fires on quoted strings).
func (e *StringEnum[T]) UnmarshalText(text []byte) error {
	v, ok := resolve[T](string(text))
	if !ok {
		return &InvalidValueError[T]{Value: string(text)}
	}
	e.set(v.String())
	e.setIndex(v.Index() + 1) // Index is 0-based; setIndex wants the 1-based slot
	return nil
}

// Scan implements sql.Scanner from a text column (string or []byte). A NULL
// (nil src) leaves the zero value; an unknown value yields *InvalidValueError[T].
func (e *StringEnum[T]) Scan(src any) error {
	if src == nil {
		return nil
	}
	var s string
	switch v := src.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		var zero T
		return fmt.Errorf("enum: cannot scan %T into %T", src, zero)
	}
	m, ok := resolve[T](s)
	if !ok {
		return &InvalidValueError[T]{Value: s}
	}
	e.set(m.String())
	e.setIndex(m.Index() + 1)
	return nil
}
