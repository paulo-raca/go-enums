package enumcheck_test

import (
	"testing"

	"github.com/paulo-raca/go-enums/enumcheck"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestEnumcheck(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), enumcheck.Analyzer, "a", "b", "c", "d")
}
