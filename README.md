# genutils

Utilities for writing Go code generators.

## Getting Started

The `genutils` command line tool is used to generate code for writing generators.
Run the following command to initialize a generator command line tool, that will be used to generate code using 2
markers.

```shell
go run github.com/alexandremahdhaoui/genutils/cmd/genutils@latest --cmd gencmd --generators="yourgen:./pkg/gen,anothergen:./pkg/gen"
```

### Cmd

```go
// cmd/yourgen/main.go
package main

import (
	genutils "github.com/alexandremahdhaoui/genutils"
	gen "github.com/alexandremahdhaoui/genutils/pkg/gen"
)

const (
	name                    = "gencmd"
	description             = "TODO: Please write a description here."
	helper                  = "TODO: Please write an example here."
	yourgenGeneratorName    = "yourgen"
	anothergenGeneratorName = "anothergen"
)

func main() {
	genutils.
		New(name).
		WithDescription(description).
		WithHelper(helper).
		WithGenerator(yourgenGeneratorName, gen.YourgenGenerator{}).
		WithGenerator(anothergenGeneratorName, gen.AnothergenGenerator{}).
		Apply().
		Run()
}
```

### Generator

```go
// pkg/yourgen/yourgen.go
package gen

import (
	genall "sigs.k8s.io/controller-tools/pkg/genall"
	markers "sigs.k8s.io/controller-tools/pkg/markers"
)

var yourgenMarkerDefinition = markers.Must(markers.MakeDefinition("gencmd:yourgen", markers.DescribesType, YourgenGenerator{}))

type YourgenGenerator struct {
	HeaderFile string `marker:",omitempty"`
	Year       string `marker:",omitempty"`
}

func (YourgenGenerator) RegisterMarkers(into *markers.Registry) error {
	if err := markers.RegisterAll(into, yourgenMarkerDefinition); err != nil {
		return err
	}
	into.AddHelp(yourgenMarkerDefinition, markers.SimpleHelp("object", ""))
	return nil
}

func (g YourgenGenerator) Generate(ctx *genall.GenerationContext) error {
	// TODO: ADD YOUR CODE HERE
	for _, root := range ctx.Roots {
		root.NeedTypesInfo()
		markerSet, err := markers.PackageMarkers(ctx.Collector, root)
		if err != nil {
			return err
		}
		markerValues := markerSet[yourgenMarkerDefinition.Name]
		if len(markerValues) == 0 {
			continue
		}
		// TODO: YOU CAN ALSO ADD YOUR CODE HERE
	}
	// TODO: OR HERE
	return nil
}
```

## Use your new generator

### Use it on a struct

```go
//+gencmd:yourgen

type YourStruct struct{}
```

### Execute your generator

```go
//go:generate go run github.com/username/repo/cmd/gencmd@latest

package yourpkg
```
