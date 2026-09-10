// Package enum is a minimal stand-in for github.com/paulo-raca/go-enums/enum,
// exposing just the shapes the analyzer keys on, so analysistest can compile
// testdata at the canonical import path without the real dependency.
package enum

type StringEnum[T any] struct{ val string }

func (e *StringEnum[T]) set(s string) { e.val = s }

type IntEnum[T any] struct{ val int }

func (e *IntEnum[T]) set(n int) { e.val = n }

func New[T any, V any](v V) T { var z T; return z }

func NextInt[T any]() T { var z T; return z }

// The cast family is generic methods on the two bases, mirroring the real
// set constraints so call sites instantiate the same type arguments
// (To=0, PTo=1) the analyzer reads. The source enum comes from the receiver
// type rather than a type argument.

func (e StringEnum[T]) TryAs[To any, PTo interface {
	*To
	set(string)
}]() (To, bool) {
	var z To
	return z, false
}

func (e StringEnum[T]) As[To any, PTo interface {
	*To
	set(string)
}]() (To, error) {
	var z To
	return z, nil
}

func (e StringEnum[T]) MustAs[To any, PTo interface {
	*To
	set(string)
}]() To {
	var z To
	return z
}

func (e IntEnum[T]) TryAs[To any, PTo interface {
	*To
	set(int)
}]() (To, bool) {
	var z To
	return z, false
}

func (e IntEnum[T]) As[To any, PTo interface {
	*To
	set(int)
}]() (To, error) {
	var z To
	return z, nil
}

func (e IntEnum[T]) MustAs[To any, PTo interface {
	*To
	set(int)
}]() To {
	var z To
	return z
}

func SameValues[A any, B any]() error { return nil }
