// Package plugin exposes the enumcheck analyzer as a golangci-lint module
// plugin. It is a separate package so the core analyzer and the standalone
// cmd/enumcheck binary stay free of the golangci-lint dependency.
//
// See ../README.md for how to wire it up with `golangci-lint custom`.
package plugin

import (
	"github.com/golangci/plugin-module-register/register"
	"github.com/paulo-raca/go-enums/enumcheck"
	"golang.org/x/tools/go/analysis"
)

func init() {
	register.Plugin("enumcheck", New)
}

// New builds the plugin. It takes no settings.
func New(any) (register.LinterPlugin, error) { return plugin{}, nil }

type plugin struct{}

func (plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{enumcheck.Analyzer}, nil
}

// GetLoadMode requests full type information, which enumcheck needs (it inspects
// types and uses analysis facts).
func (plugin) GetLoadMode() string { return register.LoadModeTypesInfo }
