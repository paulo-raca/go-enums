package enumcheck

// This file registers the analyzer as a golangci-lint module plugin. It lives
// in the analyzer package itself, so importing enumcheck (or installing
// cmd/enumcheck) pulls in plugin-module-register and runs the registration in
// init — a ~6 KB, no-op-at-runtime cost. See README.md for the golangci-lint
// setup; the .custom-gcl.yml `import` is this package.

import (
	"github.com/golangci/plugin-module-register/register"
	"golang.org/x/tools/go/analysis"
)

func init() {
	register.Plugin("enumcheck", newPlugin)
}

// newPlugin builds the golangci-lint plugin. It takes no settings.
func newPlugin(any) (register.LinterPlugin, error) { return plugin{}, nil }

type plugin struct{}

func (plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{Analyzer}, nil
}

// GetLoadMode requests full type information, which enumcheck needs (it inspects
// types and uses analysis facts).
func (plugin) GetLoadMode() string { return register.LoadModeTypesInfo }
