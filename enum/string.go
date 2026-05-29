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
	// pos is the 1-based registration order; 0 only for the Go zero value. It
	// both orders members and marks the zero value invalid, so StringEnum{} stays
	// distinct from New[T]("") — a member backed by the empty string is still
	// tellable from an unset field.
	pos int
}

// String returns the member's canonical string, or "<invalid T>" for the zero
// value. Implements fmt.Stringer. The check is the lock-free pos field, so the
// common path stays cheap (only the rare zero-value path reflects on T).
func (e StringEnum[T]) String() string {
	if e.pos == 0 {
		return invalidString[T]()
	}
	return e.val
}

// Value returns the backing string (identical to String for a valid member).
func (e StringEnum[T]) Value() string { return e.val }

// IsValid reports whether e is a real member rather than the Go zero value. It
// is the lock-free pos field, so it is cheap; it does not consult the registry.
// Mirrors reflect.Value.IsValid.
func (e StringEnum[T]) IsValid() bool { return e.pos != 0 }

// Position returns the member's 0-based registration order, or -1 for the zero
// value. Members are ordered by registration; Values returns them in this order.
// (Internally pos is 1-based so 0 marks the zero value; Position subtracts one.)
func (e StringEnum[T]) Position() int { return e.pos - 1 }

// MarshalText implements encoding.TextMarshaler. encoding/json uses this
// automatically (quoting the result) when no MarshalJSON is present, so a
// StringEnum encodes as a JSON string and works as a JSON map key. Encoding the
// zero value yields *ZeroMarshalError[T].
func (e StringEnum[T]) MarshalText() ([]byte, error) {
	if e.pos == 0 {
		return nil, &ZeroMarshalError[T]{}
	}
	return []byte(e.val), nil
}

// set is the unexported value write path. Promoted onto *T it keeps this
// package's identity, which is what closes the set while still letting New
// construct values for T defined in another package. IntEnum carries a set(int)
// of the same name; that shared name is what lets a single New serve both bases.
func (e *StringEnum[T]) set(s string) { e.val = s }

// setPos records the 1-based registration position; see registerLocked. Paired
// with set in the New/NextInt constructor constraints.
func (e *StringEnum[T]) setPos(p int) { e.pos = p }

// isEnumMember is the unexported marker required by the Enum constraint. Only
// StringEnum and IntEnum define it, so embedding one of them is what makes a
// type an Enum — an arbitrary comparable Stringer cannot qualify.
func (StringEnum[T]) isEnumMember() {}

// UnmarshalText implements encoding.TextUnmarshaler. It resolves text against
// T's registered members and copies in the canonical value and position, or
// returns *InvalidValueError[T]. JSON null is left untouched (the text path only
// fires on quoted strings).
func (e *StringEnum[T]) UnmarshalText(text []byte) error {
	v, ok := lookup[T](string(text))
	if !ok {
		return &InvalidValueError[T]{Value: string(text)}
	}
	e.set(v.String())
	e.setPos(v.Position() + 1) // Position is 0-based; setPos wants the 1-based slot
	return nil
}
