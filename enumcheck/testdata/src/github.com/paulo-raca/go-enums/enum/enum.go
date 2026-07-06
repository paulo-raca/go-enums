// Package enum is a minimal stand-in for github.com/paulo-raca/go-enums/enum,
// exposing just the shapes the analyzer keys on, so analysistest can compile
// testdata at the canonical import path without the real dependency.
package enum

type StringEnum[T any] struct{ val string }

func (e StringEnum[T]) get() string   { return e.val }
func (e *StringEnum[T]) set(s string) { e.val = s }

type IntEnum[T any] struct{ val int }

func (e IntEnum[T]) get() int   { return e.val }
func (e *IntEnum[T]) set(n int) { e.val = n }

func New[T any, V any](v V) T { var z T; return z }

func NextInt[T any]() T { var z T; return z }

// The cast family mirrors the real get/set constraints so call sites infer
// the same type arguments (To=0, V=1, From=2, PTo=3) the analyzer reads.

func LookupAs[To any, V any, From interface{ get() V }, PTo interface {
	*To
	set(V)
}](from From) (To, bool) {
	var z To
	return z, false
}

func As[To any, V any, From interface{ get() V }, PTo interface {
	*To
	set(V)
}](from From) (To, error) {
	var z To
	return z, nil
}

func MustAs[To any, V any, From interface{ get() V }, PTo interface {
	*To
	set(V)
}](from From) To {
	var z To
	return z
}

func SameValues[A any, B any]() error { return nil }
