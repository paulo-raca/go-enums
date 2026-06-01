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
//   - database/sql: driver.Valuer + sql.Scanner (StringEnum as text, IntEnum as
//     int64); nullable columns use a *T pointer
//   - typed *InvalidValueError[T] / *ZeroMarshalError[T] errors  (work with errors.As)
//   - IsValid()/IsZero() and Index() methods on each member (Index is 0-based
//     registration order; -1 marks the zero value)
//   - Compare() ordering members by Index — Go has no operator overloading,
//     so a < b is a.Compare(b) < 0; sort with slices.SortFunc(xs, T.Compare)
//   - Values[T](); and four flavors of value lookup: Valid[T] (bool),
//     Lookup[T] (T, bool), Parse[T] (T, error), MustParse[T] (T, panics)
//   - typed tags via New(v, Tag(g)...): query with ValuesWithTag[T] (one tag),
//     ValuesWithAnyTags[T] (union), ValuesWithAllTags[T] (intersection); plus
//     member methods HasTag(tag)/Tags()
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
// Requires Go 1.24+.
package enum

import (
	"fmt"
	"reflect"
	"slices"
	"sync"
)

// Enum is the constraint satisfied by any type that embeds StringEnum[Self] or
// IntEnum[Self]. Beyond being comparable and a fmt.Stringer, it requires the
// unexported isEnumMember marker that only those two bases provide, so an
// arbitrary comparable Stringer cannot masquerade as an enum: the set of enum
// types is closed at the constraint level. Index exposes the 0-based
// registration order (-1 for the zero value).
type Enum interface {
	comparable
	fmt.Stringer
	isEnumMember()
	Index() int
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
// 0-based slot (Index-1); names/ints double as the duplicate-detection sets,
// keyed by the backing value rather than the whole member (which now carries a
// position and so is not a stable dedup key). A member lives in exactly one of
// names/ints depending on its base, never both.
type bucket struct {
	order      []any          // members in registration order, for Values
	names      map[string]any // value -> member, for Lookup/Valid + dedup (StringEnum only)
	ints       map[int]any    // value -> member, for Lookup/Valid + dedup (IntEnum only)
	maxInt     int            // highest value seen so far (IntEnum only)
	tagsBySlot [][]any        // tags per member, parallel to order (slot = Index)
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
	setIndex(int)
}](t *T, hasInt bool, ival int, tags []any) {
	b := bucketOf(reflect.TypeFor[T]())
	// Index is the next free slot. Assign it before reading String() so the
	// member no longer renders as the zero-value placeholder.
	PT(t).setIndex(len(b.order) + 1)
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
	b.tagsBySlot = append(b.tagsBySlot, tags) // kept parallel to order
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
//
// Optional Tag options attach tags to the member; see Tag and the
// ValuesWithTag/ValuesWithAnyTags/ValuesWithAllTags queries.
func New[T Enum, V any, PT interface {
	*T
	set(V)
	setIndex(int)
}](v V, opts ...Option) T {
	var t T
	PT(&t).set(v)
	tags := tagsOf(opts)
	mu.Lock()
	defer mu.Unlock()
	if iv, ok := any(v).(int); ok {
		registerLocked[T, PT](&t, true, iv, tags)
	} else {
		registerLocked[T, PT](&t, false, 0, tags)
	}
	return t
}

// Option configures a member at construction. The only option today is Tag.
type Option struct{ tag any }

// Tag attaches a tag to the member being constructed. A tag is a value of any
// comparable type; use a named type — a small `type Group string`, or a go-enums
// enum — so tags are typo-proof and can be cross-queried with ValuesWithTag/ValuesWithAllTags. A
// member may carry tags of several different types.
//
//	A1 = enum.New[Suit]("a.1", enum.Tag(GroupA), enum.Tag(Tier1))
func Tag[G comparable](g G) Option { return Option{tag: g} }

// tagsOf flattens and de-duplicates the tags carried by opts.
func tagsOf(opts []Option) []any {
	if len(opts) == 0 {
		return nil
	}
	tags := make([]any, 0, len(opts))
	for _, o := range opts {
		tags = appendUnique(tags, o.tag)
	}
	return tags
}

// ValuesWithTag returns the members of T tagged with tag, in registration order.
// It is the single-tag form; for several tags use ValuesWithAnyTags (union) or
// ValuesWithAllTags (intersection).
//
//	enum.ValuesWithTag[Suit](GroupA) // members tagged GroupA
func ValuesWithTag[T Enum](tag any) []T { return queryTags[T]([]any{tag}, false) }

// ValuesWithAnyTags returns the members of T tagged with at least one of the
// given tags (set union), deduplicated and in registration order. Tags may be of
// different types in one query (they are comparable by construction; see Tag).
// With no tags it returns nil.
//
//	enum.ValuesWithAnyTags[Suit](GroupA, Tier1)  // members in GroupA OR Tier1
//	enum.ValuesWithAnyTags[Card](GroupA, Common) // mixed tag types
func ValuesWithAnyTags[T Enum](tags ...any) []T { return queryTags[T](tags, false) }

// ValuesWithAllTags returns the members of T tagged with every one of the given
// tags (set intersection), in registration order. Tags may be of different
// types. With no tags it returns nil.
//
//	enum.ValuesWithAllTags[Suit](GroupA, Tier1) // members in GroupA AND Tier1
func ValuesWithAllTags[T Enum](tags ...any) []T { return queryTags[T](tags, true) }

func queryTags[T Enum](tags []any, needAll bool) []T {
	if len(tags) == 0 {
		return nil
	}
	// De-duplicate query tags so ValuesWithAllTags's threshold counts distinct tags.
	distinct := make([]any, 0, len(tags))
	for _, t := range tags {
		distinct = appendUnique(distinct, t)
	}
	tags = distinct

	mu.RLock()
	defer mu.RUnlock()
	b := reg[reflect.TypeFor[T]()]
	if b == nil {
		return nil
	}
	var out []T
	for i, m := range b.order {
		have := 0
		for _, qt := range tags {
			if slices.Contains(b.tagsBySlot[i], qt) {
				have++
			}
		}
		if (needAll && have == len(tags)) || (!needAll && have > 0) {
			out = append(out, m.(T))
		}
	}
	return out
}

// hasTag reports whether the member at 1-based position index carries tag.
func hasTag[T Enum](index int, tag any) bool {
	if index == 0 {
		return false
	}
	mu.RLock()
	defer mu.RUnlock()
	b := reg[reflect.TypeFor[T]()]
	return b != nil && index <= len(b.tagsBySlot) && slices.Contains(b.tagsBySlot[index-1], tag)
}

// memberTags returns a copy of the tags of the member at 1-based position index.
func memberTags[T Enum](index int) []any {
	if index == 0 {
		return nil
	}
	mu.RLock()
	defer mu.RUnlock()
	b := reg[reflect.TypeFor[T]()]
	if b == nil || index > len(b.tagsBySlot) {
		return nil
	}
	return append([]any(nil), b.tagsBySlot[index-1]...)
}

// appendUnique appends v to xs unless it is already present (order-preserving
// de-dup over a small slice). slices.Contains handles membership; v is
// comparable by construction (see Tag), so the == comparison can't panic.
func appendUnique(xs []any, v any) []any {
	if slices.Contains(xs, v) {
		return xs
	}
	return append(xs, v)
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
// with New and Lookup, v is a string for a StringEnum or an int for an
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
	_, ok := resolve[T](v)
	return ok
}

// Lookup resolves the backing value v to a registered member of T: a string
// for a StringEnum, or an int for an IntEnum:
//
//	s, ok := enum.Lookup[Suit]("hearts")
//	c, ok := enum.Lookup[Color](2)
//
// V is inferred from the argument. The set(V) constraint ties V to T's backing
// type exactly as New does, so passing the wrong type for a given enum — e.g.
// enum.Lookup[Suit](5) or enum.Lookup[Color]("2") — is a compile error,
// not a runtime miss.
func Lookup[T Enum, V any, PT interface {
	*T
	set(V)
}](v V) (T, bool) {
	return resolve[T](v)
}

// Parse resolves the backing value v to a registered member of T. Same dispatch
// as Lookup, but returns *InvalidValueError[T] instead of (T, bool) — so the
// common callsite ("look up, return err with %w") composes with errors.As and
// the existing typed-error machinery.
//
//	v, err := enum.Parse[Suit]("hearts")
//	v, err := enum.Parse[Priority](10)
func Parse[T Enum, V any, PT interface {
	*T
	set(V)
}](v V) (T, error) {
	m, ok := resolve[T](v)
	if !ok {
		return m, &InvalidValueError[T]{Value: fmt.Sprint(v)}
	}
	return m, nil
}

// MustParse is the panicking sibling of Parse — for var-block / struct-literal
// initialisers where (T, error) is awkward. Panics with *InvalidValueError[T].
//
//	var EdgeLabelHasMember = enum.MustParse[EdgeLabel]("HAS_MEMBER") // unusual
//	conn.SourceType = enum.MustParse[SourceType](dbRow.SourceType)   // typical
func MustParse[T Enum, V any, PT interface {
	*T
	set(V)
}](v V) T {
	m, err := Parse[T, V, PT](v)
	if err != nil {
		panic(err)
	}
	return m
}

// resolve is the unconstrained resolver shared by Lookup, Parse, Valid, and the
// Unmarshal methods. It carries no set(V) constraint, so the Unmarshal methods —
// whose T is known only to be an Enum and cannot prove *T has the setter — can
// still call it. Lookup/Parse/Valid layer the compile-time type check on top.
func resolve[T Enum, V any](v V) (T, bool) {
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
