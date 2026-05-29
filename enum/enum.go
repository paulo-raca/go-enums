// Package enum provides generic, closed-set, value-backed enum types that
// kill the per-type boilerplate (String, marshalling, validation, listing).
//
// Two embeddable bases are offered:
//
//	type Suit  struct{ enum.StringEnum[Suit] }
//	type Color struct{ enum.IntEnum[Color] }
//
// Members are declared once, in a var block, and that is all the boilerplate:
//
//	var (
//		Hearts   = enum.New[Suit]("hearts")
//		Diamonds = enum.New[Suit]("diamonds")
//	)
//
//	var (
//		Red   = enum.NextInt[Color]() // 0  (iota-like auto-increment)
//		Green = enum.NextInt[Color]() // 1
//		Blue  = enum.NextInt[Color]() // 2
//	)
//
// You get for free, per type:
//
//   - String()                              (fmt.Stringer)
//   - MarshalText / UnmarshalText           (encoding.Text{Marshaler,Unmarshaler})
//   - JSON: StringEnum as a string (via the text interfaces), IntEnum as a
//     number (via Marshal/UnmarshalJSON)
//   - typed *InvalidValueError[T] / *ZeroMarshalError[T] errors  (work with errors.As)
//   - IsValid() and Position() methods on each member (Position is 0-based
//     registration order; -1 marks the zero value)
//   - Values[T](), Valid[T](value), FromValue[T](value)
//
// A constructed member is always distinct from the zero value — even one backed
// by "" or 0 — so MyEnum{} works as an "unset" sentinel (detect it with == or
// Valid). The zero value renders as "<invalid T>" (e.g. "<invalid Suit>") from
// String and is refused by the Marshal methods (its "" / 0 output would not
// round-trip).
//
// Closure: the backing field and its setter are unexported, so New (and the
// iota-like NextInt) are the only ways to mint a member. Code in any package may
// declare enum types and call them, but cannot forge arbitrary values — that
// is a compile-time error.
//
// Members persist only their backing value (the string for StringEnum, the
// int for IntEnum); names are not stored separately. The registry is
// mutex-guarded, so concurrent reads and runtime registration are safe, though
// registration is normally an init-time var-block affair.
//
// Requires Go 1.22+ (reflect.TypeFor).
package enum

import (
	"fmt"
	"reflect"
	"sync"
)

// Enum is the constraint satisfied by any type that embeds StringEnum[Self] or
// IntEnum[Self]. Beyond being comparable and a fmt.Stringer, it requires the
// unexported isEnumMember marker that only those two bases provide, so an
// arbitrary comparable Stringer cannot masquerade as an enum: the set of enum
// types is closed at the constraint level. Position exposes the 0-based
// registration order (-1 for the zero value).
type Enum interface {
	comparable
	fmt.Stringer
	isEnumMember()
	Position() int
}

// InvalidValueError is returned when an input does not name a registered
// member of T. Match it with errors.As(err, new(*enum.InvalidValueError[YourEnum])).
type InvalidValueError[T Enum] struct {
	Value string
}

func (e *InvalidValueError[T]) Error() string {
	var zero T
	return fmt.Sprintf("invalid %T: %q", zero, e.Value)
}

// invalidString is what String() renders for the zero value, which names no
// registered member: a placeholder naming the concrete enum type, e.g.
// "<invalid Suit>".
func invalidString[T Enum]() string {
	return "<invalid " + reflect.TypeFor[T]().Name() + ">"
}

// ZeroMarshalError is returned by the Marshal methods when asked to encode the
// zero value of T: it is not a registered member and the output ("" / 0) would
// not round-trip, so it is refused rather than emitted for Unmarshal to reject.
// Match it with errors.As(err, new(*enum.ZeroMarshalError[YourEnum])).
type ZeroMarshalError[T Enum] struct{}

func (e *ZeroMarshalError[T]) Error() string {
	var zero T
	return fmt.Sprintf("enum: refusing to marshal zero value of %T", zero)
}

// bucket holds the registered members of a single enum type. order indexes by
// 0-based slot (Position-1); names/ints double as the duplicate-detection sets,
// keyed by the backing value rather than the whole member (which now carries a
// position and so is not a stable dedup key). A member lives in exactly one of
// names/ints depending on its base, never both.
type bucket struct {
	order  []any          // members in registration order, for Values
	names  map[string]any // value -> member, for FromValue/Valid + dedup (StringEnum only)
	ints   map[int]any    // value -> member, for FromValue/Valid + dedup (IntEnum only)
	maxInt int            // highest value seen so far (IntEnum only)
}

var (
	mu  sync.RWMutex
	reg = map[reflect.Type]*bucket{}
)

func bucketOf(t reflect.Type) *bucket {
	b := reg[t]
	if b == nil {
		b = &bucket{
			names: map[string]any{},
			ints:  map[int]any{},
		}
		reg[t] = b
	}
	return b
}

// registerLocked assigns t its 1-based insertion position, then records it. The
// caller must hold mu for writing. Registering the same backing value twice for
// a type is a programmer error (a copy-pasted member, a clashing int) and panics.
func registerLocked[T Enum, PT interface {
	*T
	setPos(int)
}](t *T, hasInt bool, ival int) {
	b := bucketOf(reflect.TypeFor[T]())
	// Position is the next free slot. Assign it before reading String() so the
	// member no longer renders as the zero-value placeholder.
	PT(t).setPos(len(b.order) + 1)
	name := (*t).String()

	dup := false
	if hasInt {
		_, dup = b.ints[ival]
	} else {
		_, dup = b.names[name]
	}
	if dup {
		panic(fmt.Sprintf("enum: duplicate registration of %T value %q", *t, name))
	}

	v := *t
	b.order = append(b.order, v)
	if hasInt {
		// Track the running maximum in O(1) so NextInt never has to scan the
		// member set, even when explicit New values are interleaved. Check
		// emptiness before inserting so the first value seeds maxInt.
		if len(b.ints) == 0 || ival > b.maxInt {
			b.maxInt = ival
		}
		b.ints[ival] = v
	} else {
		b.names[name] = v
	}
}

// New constructs, registers, and returns an enum member. It is the single
// constructor for both bases: pass a string for a StringEnum, or an int for an
// IntEnum. Only T need be named at the call site — V is inferred from the
// argument and PT (*T) from the constraint:
//
//	Hearts = enum.New[Suit]("hearts")
//	Low    = enum.New[Priority](10)
//
// The set(V) constraint is what dispatches: StringEnum has set(string) and
// IntEnum has set(int), so the right one is selected at compile time and a
// mismatched value type is a compile error. For an IntEnum, an explicit value
// also advances the auto-increment counter so a following NextInt yields one
// past the highest value. Registering the same value twice for a type panics.
func New[T Enum, V any, PT interface {
	*T
	set(V)
	setPos(int)
}](v V) T {
	var t T
	PT(&t).set(v)
	mu.Lock()
	defer mu.Unlock()
	if iv, ok := any(v).(int); ok {
		registerLocked[T, PT](&t, true, iv)
	} else {
		registerLocked[T, PT](&t, false, 0)
	}
	return t
}

// nextIntLocked reports the next auto-increment value for T: one past the
// highest value registered so far, or 0 when no int members exist yet. The
// caller must hold mu (NextInt holds it for writing across the read-and-register
// so the value can't be handed out twice).
func nextIntLocked[T Enum]() int {
	if b := reg[reflect.TypeFor[T]()]; b != nil && len(b.ints) != 0 {
		return b.maxInt + 1
	}
	return 0
}

// Values returns T's registered members in registration order.
func Values[T Enum]() []T {
	mu.RLock()
	defer mu.RUnlock()
	b := reg[reflect.TypeFor[T]()]
	if b == nil {
		return nil
	}
	out := make([]T, len(b.order))
	for i, v := range b.order {
		out[i] = v.(T)
	}
	return out
}

// Valid reports whether the backing value v names a registered member of T. As
// with New and FromValue, v is a string for a StringEnum or an int for an
// IntEnum, and the set(V) constraint makes a wrong type a compile error:
//
//	enum.Valid[Suit]("hearts") // true
//	enum.Valid[Color](2)       // true
//
// To test a member value itself (a constructed member vs the zero value), call
// its IsValid method instead: v.IsValid().
func Valid[T Enum, V any, PT interface {
	*T
	set(V)
}](v V) bool {
	_, ok := lookup[T](v)
	return ok
}

// FromValue resolves the backing value v to a registered member of T: a string
// for a StringEnum, or an int for an IntEnum:
//
//	s, ok := enum.FromValue[Suit]("hearts")
//	c, ok := enum.FromValue[Color](2)
//
// V is inferred from the argument. The set(V) constraint ties V to T's backing
// type exactly as New does, so passing the wrong type for a given enum — e.g.
// enum.FromValue[Suit](5) or enum.FromValue[Color]("2") — is a compile error,
// not a runtime miss.
func FromValue[T Enum, V any, PT interface {
	*T
	set(V)
}](v V) (T, bool) {
	return lookup[T](v)
}

// lookup is the unconstrained resolver shared by FromValue and the Unmarshal
// methods. It carries no set(V) constraint, so the Unmarshal methods — whose T
// is known only to be an Enum and cannot prove *T has the setter — can still
// call it. FromValue layers the compile-time type check on top.
func lookup[T Enum, V any](v V) (T, bool) {
	mu.RLock()
	defer mu.RUnlock()
	b := reg[reflect.TypeFor[T]()]
	if b == nil {
		var zero T
		return zero, false
	}
	var raw any
	var ok bool
	switch val := any(v).(type) {
	case string:
		raw, ok = b.names[val]
	case int:
		raw, ok = b.ints[val]
	}
	if !ok {
		var zero T
		return zero, false
	}
	return raw.(T), true
}
