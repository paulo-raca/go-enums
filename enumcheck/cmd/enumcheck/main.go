// Command enumcheck runs the enumcheck analyzer.
//
// Install:
//
//	go install github.com/paulo-raca/go-enums/enumcheck/cmd/enumcheck@latest
//
// Run standalone:
//
//	enumcheck ./...
//
// Or through go vet:
//
//	go vet -vettool=$(which enumcheck) ./...
package main

import (
	"github.com/paulo-raca/go-enums/enumcheck"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(enumcheck.Analyzer) }
