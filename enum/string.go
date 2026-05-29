package enum

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
	// present is false only for the Go zero value; set() marks every member it
	// builds. It is what makes StringEnum{} distinct from New[T](""), so a
	// member backed by the empty string is still tellable from an unset field.
	present bool
}

// String returns the member's canonical string. Implements fmt.Stringer.
func (e StringEnum[T]) String() string { return e.val }

// Value returns the backing string (identical to String for StringEnum).
func (e StringEnum[T]) Value() string { return e.val }

// MarshalText implements encoding.TextMarshaler. encoding/json uses this
// automatically (quoting the result) when no MarshalJSON is present, so a
// StringEnum encodes as a JSON string and works as a JSON map key.
func (e StringEnum[T]) MarshalText() ([]byte, error) { return []byte(e.val), nil }

// set is the unexported write path. Promoted onto *T it keeps this package's
// identity, which is what closes the set while still letting New construct
// values for T defined in another package. IntEnum carries a set(int) of the
// same name; that shared name is what lets a single New serve both bases.
func (e *StringEnum[T]) set(s string) { e.val, e.present = s, true }

// isEnumMember is the unexported marker required by the Enum constraint. Only
// StringEnum and IntEnum define it, so embedding one of them is what makes a
// type an Enum — an arbitrary comparable Stringer cannot qualify.
func (StringEnum[T]) isEnumMember() {}

// UnmarshalText implements encoding.TextUnmarshaler. It resolves text against
// T's registered members and copies in the canonical value, or returns
// *InvalidValueError[T]. JSON null is left untouched (the text path only fires on
// quoted strings).
func (e *StringEnum[T]) UnmarshalText(text []byte) error {
	v, ok := lookup[T](string(text))
	if !ok {
		return &InvalidValueError[T]{Value: string(text)}
	}
	e.set(v.String())
	return nil
}
