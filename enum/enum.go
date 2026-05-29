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
//   - JSON marshal/unmarshal:
//   - StringEnum encodes as a JSON string (via the text interfaces)
//   - IntEnum    encodes as a JSON number (via Marshal/UnmarshalJSON)
//   - a typed *InvalidValueError[T] on bad input  (works with errors.As)
//   - Values[T](), Valid[T](), FromValue[T]()
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

// Enum is satisfied by any type embedding StringEnum[Self] or IntEnum[Self].
// It is the constraint used by every generic helper in this package.
type Enum interface {
	comparable
	fmt.Stringer
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

// bucket holds the registered members of a single enum type.
type bucket struct {
	order  []any            // members in registration order, for Values
	valid  map[any]struct{} // membership, for Valid
	names  map[string]any   // String() -> member, for FromValue(string)
	ints   map[int]any      // value -> member, for FromValue(int) (IntEnum only)
	maxInt int              // highest value seen so far (IntEnum only)
}

var (
	mu  sync.RWMutex
	reg = map[reflect.Type]*bucket{}
)

func bucketOf(t reflect.Type) *bucket {
	b := reg[t]
	if b == nil {
		b = &bucket{
			valid: map[any]struct{}{},
			names: map[string]any{},
			ints:  map[int]any{},
		}
		reg[t] = b
	}
	return b
}

// register records a member under lock. Registering the same value twice for a
// type is a programmer error (a copy-pasted member, a clashing int) and panics.
func register[T Enum](v T, hasInt bool, ival int) {
	mu.Lock()
	defer mu.Unlock()
	b := bucketOf(reflect.TypeFor[T]())
	if _, dup := b.valid[v]; dup {
		panic(fmt.Sprintf("enum: duplicate registration of %T value %q", v, v.String()))
	}
	b.valid[v] = struct{}{}
	b.order = append(b.order, v)
	b.names[v.String()] = v
	if hasInt {
		// Track the running maximum in O(1) so NextInt never has to scan the
		// member set, even when explicit New values are interleaved. Check
		// emptiness before inserting so the first value seeds maxInt.
		if len(b.ints) == 0 || ival > b.maxInt {
			b.maxInt = ival
		}
		b.ints[ival] = v
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
}](v V) T {
	var t T
	PT(&t).set(v)
	if iv, ok := any(v).(int); ok {
		register[T](t, true, iv)
	} else {
		register[T](t, false, 0)
	}
	return t
}

// nextInt reports the next auto-increment value for T: one past the highest
// value registered so far, or 0 when no int members exist yet.
func nextInt[T Enum]() int {
	mu.RLock()
	defer mu.RUnlock()
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

// Valid reports whether v is a registered member of T.
func Valid[T Enum](v T) bool {
	mu.RLock()
	defer mu.RUnlock()
	b := reg[reflect.TypeFor[T]()]
	if b == nil {
		return false
	}
	_, ok := b.valid[v]
	return ok
}

// FromValue resolves v to a registered member of T. It is the single lookup for
// both bases: pass a string to match by the String() projection (for IntEnum,
// that is the decimal form), or an int to match an IntEnum value directly:
//
//	s, ok := enum.FromValue[Suit]("hearts")
//	c, ok := enum.FromValue[Color](2)
//
// V is inferred from the argument. The set(V) constraint ties V to T's backing
// type exactly as New does, so passing the wrong type for a given enum — e.g.
// enum.FromValue[Suit](5) — is a compile error, not a runtime miss.
func FromValue[T Enum, V any, PT interface {
	*T
	set(V)
}](v V) (T, bool) {
	return lookup[T](v)
}

// lookup is the unconstrained resolver shared by FromValue and the Unmarshal
// methods. It carries no set(V) constraint, so the Unmarshal methods — whose T
// is known only to be an Enum and cannot prove *T has set — can still call it.
// FromValue layers the compile-time type check on top.
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
