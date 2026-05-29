// Package enum is a minimal stand-in for github.com/paulo-raca/go-enums/enum,
// exposing just the shapes the analyzer keys on, so analysistest can compile
// testdata at the canonical import path without the real dependency.
package enum

type StringEnum[T any] struct{ val string }

type IntEnum[T any] struct{ val int }

func New[T any, V any](v V) T { var z T; return z }

func NextInt[T any]() T { var z T; return z }
