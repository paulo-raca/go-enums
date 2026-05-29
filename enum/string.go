package enum

// StringEnum is embedded (parameterised over the embedding type) to turn a
// struct into a string-backed enum member:
//
//	type Suit struct{ enum.StringEnum[Suit] }
//
// T appears only in method signatures — it is a phantom type parameter carrying
// the concrete identity so UnmarshalText can resolve against the right member
// set and return *InvalidError[T]. It is never a stored field.
type StringEnum[T Enum] struct {
	val string
}

// String returns the member's canonical string. Implements fmt.Stringer.
func (e StringEnum[T]) String() string { return e.val }

// Value returns the backing string (identical to String for StringEnum).
func (e StringEnum[T]) Value() string { return e.val }

// MarshalText implements encoding.TextMarshaler. encoding/json uses this
// automatically (quoting the result) when no MarshalJSON is present, so a
// StringEnum encodes as a JSON string and works as a JSON map key.
func (e StringEnum[T]) MarshalText() ([]byte, error) { return []byte(e.val), nil }

// setString is the unexported write path. Promoted onto *T it keeps this
// package's identity, which is what closes the set while still letting
// NewString construct values for T defined in another package.
func (e *StringEnum[T]) setString(s string) { e.val = s }

// UnmarshalText implements encoding.TextUnmarshaler. It resolves text against
// T's registered members and copies in the canonical value, or returns
// *InvalidError[T]. JSON null is left untouched (the text path only fires on
// quoted strings).
func (e *StringEnum[T]) UnmarshalText(text []byte) error {
	v, ok := FromString[T](string(text))
	if !ok {
		return &InvalidError[T]{Value: string(text)}
	}
	e.val = v.String()
	return nil
}

// NewString constructs, registers, and returns a string-backed enum member. It
// is the only constructor for StringEnum: the value's string is also its name.
// Only T need be named at the call site — PT (*T) is resolved by constraint
// type inference:
//
//	Hearts = enum.NewString[Suit]("hearts")
//
// Duplicate registrations (same T, same string) are ignored.
func NewString[T Enum, PT interface {
	*T
	setString(string)
}](s string) T {
	var v T
	PT(&v).setString(s)
	register[T](v, false, 0)
	return v
}
