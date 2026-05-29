// Package enum provides generic, closed-set, value-backed enum types that
// kill the per-type boilerplate (String, marshalling, validation, listing).
//
// Two embeddable bases are offered:
//
//	type AIStopReason struct{ enum.StringEnum[AIStopReason] }
//	type Color        struct{ enum.IntEnum[Color] }
//
// Members are declared once, in a var block, and that is all the boilerplate:
//
//	var (
//		AIStopReasonEndTurn   = enum.NewString[AIStopReason]("end_turn")
//		AIStopReasonMaxTokens = enum.NewString[AIStopReason]("max_tokens")
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
//   - a typed *InvalidError[T] on bad input  (works with errors.As)
//   - Values[T](), Valid[T](), FromString[T](), FromInt[T]()
//
// Closure: the backing field and its setter are unexported, so the New*
// constructors are the only way to mint a member. Code in any package may
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

// InvalidError is returned when an input does not name a registered member of
// T. Match it with errors.As(err, new(*enum.InvalidError[YourEnum])).
type InvalidError[T Enum] struct {
	Value string
}

func (e *InvalidError[T]) Error() string {
	var zero T
	return fmt.Sprintf("invalid %T: %q", zero, e.Value)
}

// bucket holds the registered members of a single enum type.
type bucket struct {
	order []any            // members in registration order, for Values
	valid map[any]struct{} // membership, for Valid
	names map[string]any   // String() -> member, for FromString
	ints  map[int]any      // value -> member, for FromInt (IntEnum only)
	next  int              // next auto-increment value (IntEnum only)
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

// register records a member under lock. Repeated identical members are ignored,
// so re-running a var block (e.g. in tests) is harmless.
func register[T Enum](v T, hasInt bool, ival int) {
	mu.Lock()
	defer mu.Unlock()
	b := bucketOf(reflect.TypeFor[T]())
	if _, dup := b.valid[v]; dup {
		return
	}
	b.valid[v] = struct{}{}
	b.order = append(b.order, v)
	b.names[v.String()] = v
	if hasInt {
		b.ints[ival] = v
		if ival >= b.next {
			b.next = ival + 1
		}
	}
}

// nextInt reports the next auto-increment value for T (0 if none registered).
func nextInt[T Enum]() int {
	mu.RLock()
	defer mu.RUnlock()
	if b := reg[reflect.TypeFor[T]()]; b != nil {
		return b.next
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

// FromString resolves s to a registered member of T by its String() projection.
// For IntEnum, that projection is the decimal form of the value.
func FromString[T Enum](s string) (T, bool) {
	mu.RLock()
	defer mu.RUnlock()
	b := reg[reflect.TypeFor[T]()]
	if b == nil {
		var zero T
		return zero, false
	}
	v, ok := b.names[s]
	if !ok {
		var zero T
		return zero, false
	}
	return v.(T), true
}

// FromInt resolves n to a registered member of T. Only meaningful for IntEnum
// types; a StringEnum type has no integer-keyed members and always misses.
func FromInt[T Enum](n int) (T, bool) {
	mu.RLock()
	defer mu.RUnlock()
	b := reg[reflect.TypeFor[T]()]
	if b == nil {
		var zero T
		return zero, false
	}
	v, ok := b.ints[n]
	if !ok {
		var zero T
		return zero, false
	}
	return v.(T), true
}
