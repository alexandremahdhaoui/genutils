/*
Copyright 2023 Alexandre Mahdhaoui

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/alexandremahdhaoui/genutils"
	"github.com/dave/jennifer/jen"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"sigs.k8s.io/controller-tools/pkg/loader"
	"strings"
)

const (
	name        = "genutils"
	description = "Utilities for writing Go code generators"
	example     = `
# genutils

Author: Alexandre Mahdhaoui

## 1. Initialize a new cmd

%[2]s

	NB: Please note the code generated by
	"genutils init-cmd" is meant to be 
	changed by the user.

## 2. Initialize a new generator



	NB: Please note the code generated by
	"genutils init-generator" is meant to
	be changed by the user.
`

	initCmdFlag      = "cmd"
	initCmdFlagShort = "c"
	initCmdUsage     = `The command below will initialize
a new cmd under "./cmd/<CMD_NAME>/main.go"

	genutils --cmd <CMD_NAME>

The command below will initialize a new cmd
and 2 generators. The 2 generators will be 
wired in the cmd.

	genutils --cmd mycmd --generators=myGenerator:./some/pkg,anotherGenerator:./an/other/pkg
`

	initGeneratorsFlag      = "generators"
	initGeneratorsFlagShort = "g"
	initGeneratorsUsage     = `The command below will initialize
a new generator under 
"./<PATH>/<GENERATOR_NAME>.go". 
Be aware this command alone will
not setup the generator in your 
cmd.

	genutils --generators "<GENERATOR_NAME>:<PATH>,<ANOTHER_GEN_NAME>:<MAYBE_ANOTHER_PATH>"
`
)

var (
	version        = "<unversioned>"
	initCmd        *string
	initGenerators *string
)

func main() {
	command := &cobra.Command{ //nolint:exhaustruct,exhaustivestruct
		Use:     name,
		Short:   description,
		Long:    description,
		Example: example,
		RunE:    runE,
		Version: version,
	}

	initCmd = new(string)
	initGenerators = new(string)

	command.Flags().StringVarP(initCmd, initCmdFlag, initCmdFlagShort, "", initCmdUsage)
	command.Flags().StringVarP(initGenerators, initGeneratorsFlag, initGeneratorsFlagShort, "", initGeneratorsUsage)

	if err := command.Execute(); err != nil {
		fmt.Printf("error while running %s:\n%s", name, err.Error()) //nolint:forbidigo
		os.Exit(1)
	}
}

// RUN COMMAND ---------------------------------------------------------------------------------------------------------

func runE(_ *cobra.Command, _ []string) error {
	cmd, err := parseCmdAndValidate(*initCmd)
	if err != nil {
		return err
	}

	generators, err := parseGeneratorsAndValidate(*initGenerators)
	if err != nil {
		return err
	}

	switch {
	case cmd != nil && len(generators) == 0:
		return generateCmd(*cmd)
	case cmd != nil:
		if err = generateGeneratorWithCmdName(generators, cmd.name); err != nil {
			return err
		}

		if err = generateCmdWithGenerators(*cmd, generators); err != nil {
			return err
		}

		return nil
	default: // cmd == nil && len(generators) > 0
		return generateGenerator(generators)
	}
}

// PARSE FLAGS AND VALIDATE --------------------------------------------------------------------------------------------

type (
	cmdFlag struct {
		name, path string
	}

	generatorFlag struct {
		name, path string
	}
)

func parseCmdAndValidate(s string) (*cmdFlag, error) {
	if s == "" {
		return nil, nil
	}

	return &cmdFlag{
		name: s,
		path: fmt.Sprintf("cmd/%s", s),
	}, nil
}

func parseGeneratorsAndValidate(s string) ([]generatorFlag, error) {
	parseGeneratorsErr := errors.Join(
		fmt.Errorf("received: %q", s),
		newInvalidFlagInputErr(initGeneratorsFlag),
		fmt.Errorf("usage: %s", initGeneratorsUsage),
	)

	generators := make([]generatorFlag, 0)

	for _, input := range strings.Split(s, ",") {
		sl := strings.Split(input, ":")
		if len(sl) != 2 {
			return nil, errors.Join(
				errors.New("expect 2 sub-arguments separated by a colon (\":\")"), parseGeneratorsErr)
		}

		genName := sl[0]
		if genName == "" {
			return nil, errors.Join(errors.New("name cannot be empty"), parseGeneratorsErr)
		}

		genPath := sl[1]
		if genPath == "" {
			return nil, errors.Join(errors.New("path cannot be empty"), parseGeneratorsErr)
		}

		if err := fileShouldNotExist(genPath); err != nil {
			return nil, errors.Join(err, parseGeneratorsErr)
		}

		generators = append(generators, generatorFlag{
			name: genName,
			path: genPath,
		})
	}

	if len(generators) == 0 {
		return nil, errors.Join(errors.New("expected at least one generator"), parseGeneratorsErr)
	}

	return generators, nil
}

func newInvalidFlagInputErr(flagName string) error {
	return fmt.Errorf("invalid input for flag \"--%s\"", flagName)
}

func fileShouldNotExist(path string) error {
	if _, err := os.Stat(path); err == nil {
		return errors.New("file already exist, the program will abort")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

// GENERATE COMMAND --------------------------------------------------------------------------------------------------

func generateCmd(cmd cmdFlag) error {
	return generateCmdWithGenerators(cmd, nil)
}

func generateCmdWithGenerators(cmd cmdFlag, generators []generatorFlag) error {
	genutilsImport := "github.com/alexandremahdhaoui/genutils"

	// genutils.New(name).
	//		WithDescription(description).
	//		WithHelper(helper).
	genutilsNew := jen.Qual(genutilsImport, "New").Call(jen.Id("name")).
		Dot("WithDescription").Call(jen.Id("description")).
		Dot("WithHelper").Call(jen.Id("helper"))

	consts := make([]jen.Code, 0)

	for _, g := range generators {
		genName := fmt.Sprintf("%sGeneratorName", g.name)
		genStruct := fmt.Sprintf("%sGenerator", genutils.Title(g.name))

		// const (
		// 		...
		//		genName = "generator-name"
		//	)
		consts = append(consts, jen.Id(genName).Op("=").Lit(g.name))

		roots, err := loader.LoadRoots(g.path)
		if err != nil {
			return err
		}

		if len(roots) == 0 {
			return fmt.Errorf("expected at least on package located in %q", g.path)
		}

		//		WithGenerator(cmdGeneratorName, cmd.CmdGenerator{}).
		//		WithGenerator(generatorGeneratorName, cmd.GeneratorGenerator{}).
		genutilsNew = genutilsNew.
			Dot("WithGenerator").
			Call(jen.Id(genName), jen.Qual(roots[0].String(), genStruct).Values())
	}

	consts = append([]jen.Code{
		jen.Id("name").Op("=").Lit(cmd.name),
		jen.Id("description").Op("=").Lit("TODO: Please write a description here."),
		jen.Id("helper").Op("=").Lit(`TODO: Please write an example here.`),
	}, consts...)

	//	const (
	//		name 		= "cmd"
	//		description = "TO\DO: Please write your description here"
	//		example     = ``
	constBlock := jen.Const().Defs(consts...)

	//		Apply().
	//		Run()
	genutilsNew.
		Dot("Apply").Call().
		Dot("Run").Call()

	f := jen.NewFilePath(cmd.path) //nolint:varnamelen

	f.Add(constBlock).Op(";").Func().Id("main").Params().Block(
		genutilsNew,
	)

	return writeFile(f, cmd.path, "main.go")
}

func writeFile(f *jen.File, pathToJoin ...string) error {
	buf := &bytes.Buffer{}
	if err := f.Render(buf); err != nil {
		return err
	}

	fp := filepath.Join(pathToJoin...)
	if err := os.MkdirAll(filepath.Dir(fp), 0755); err != nil { //nolint:gofumpt
		return err
	}

	return os.WriteFile(fp, buf.Bytes(), 0644) //nolint:gosec,gofumpt
}

// GENERATE GENERATOR --------------------------------------------------------------------------------------------------

func generateGenerator(generators []generatorFlag) error {
	return generateGeneratorWithCmdName(generators, "")
}

//nolint:funlen
func generateGeneratorWithCmdName(generators []generatorFlag, cmdName string) error {
	for _, g := range generators {
		f := jen.NewFilePath(g.path) //nolint:varnamelen

		marker := g.name
		if cmdName != "" {
			marker = fmt.Sprintf("%s:%s", cmdName, marker)
		}

		markerLit := jen.Lit(marker)

		generatorNameTitle := fmt.Sprintf("%sGenerator", genutils.Title(g.name))
		markersPath := "sigs.k8s.io/controller-tools/pkg/markers"
		genallPath := "sigs.k8s.io/controller-tools/pkg/genall"

		omitemptyMarkerTag := map[string]string{"marker": ",omitempty"}

		markerDefName := fmt.Sprintf("%sMarkerDefinition", g.name)

		// var ContainerMarkerDefinition = markers.Must(
		// markers.MakeDefinition(markerName(DIMarkerName, ContainerMarkerName), markers.DescribesPackage, Container{}))
		f.Var().
			Id(markerDefName).
			Op("=").
			Qual(markersPath, "Must").
			Call(
				jen.
					Qual(markersPath, "MakeDefinition").
					Call(
						markerLit,
						jen.Qual(markersPath, "DescribesType"),
						jen.Id(generatorNameTitle).Values(),
					),
			)

		f.Type().
			Id(generatorNameTitle).
			Struct(
				jen.Id("HeaderFile").String().Tag(omitemptyMarkerTag),
				jen.Id("Year").String().Tag(omitemptyMarkerTag),
			)

		// func (ContainerGenerator) RegisterMarkers(into *markers.Registry) error {
		//	if err := markers.RegisterAll(into, ContainerMarkerDefinition); err != nil {
		//		return err //nolint:wrapcheck
		//	}
		//
		//	into.AddHelp(ContainerMarkerDefinition, markers.SimpleHelp("object", ""))
		//
		//	return nil
		// }

		f.Func().
			Params(jen.Id(generatorNameTitle)).
			Id("RegisterMarkers").
			Params(jen.Id("into").Add(jen.Op("*"), jen.Qual(markersPath, "Registry"))).
			Error().
			Block(
				jen.If(
					jen.Id("err").
						Op(":=").
						Qual(markersPath, "RegisterAll").
						Call(jen.Id("into"), jen.Id(markerDefName)),
					jen.Id("err").Op("!=").
						Nil(),
				).Block(
					jen.Return(jen.Err()),
				),
				jen.Id("into").Dot("AddHelp").
					Call(
						jen.Id(markerDefName),
						jen.Qual(markersPath, "SimpleHelp").Call(jen.Lit("object"), jen.Lit("")),
					),
				jen.Return(jen.Nil()),
			)

		// func (g ContainerGenerator) Generate(ctx *genall.GenerationContext) error {
		//  	// ADD YOUR CODE HERE
		// 		for _, root := range ctx.Roots {
		// 			root.NeedTypesInfo()
		//
		// 			markerSet, err := markers.PackageMarkers(ctx.Collector, root)
		// 			if err != nil {
		// 				root.AddError(err)
		// 			}
		//
		// 			markerValues := markerSet[ContainerMarkerDefinition.Name]
		// 			if len(markerValues) == 0 {
		// 				continue
		// 			}
		//
		//  	    // OR HERE
		// 		}
		//  	// OR ALSO HERE
		// 		return nil
		//  }

		ifErrNotNilReturnErr := jen.If(jen.Id("err").Op("!=").Nil()).Block(
			jen.Return(jen.Id("err")))

		f.Func().
			Params(jen.Id("g").Id(generatorNameTitle)).
			Id("Generate").
			Params(jen.Id("ctx").Add(jen.Op("*"), jen.Qual(genallPath, "GenerationContext"))).
			Error().
			Block(
				jen.Comment("TODO: ADD YOUR CODE HERE"),
				jen.For(
					jen.Id("_").Op(",").Id("root").Op(":=").
						Range().Id("ctx").Dot("Roots"),
				).Block(
					jen.Id("root").Dot("NeedTypesInfo").Call(),
					jen.List(jen.Id("markerSet"), jen.Err()).Op(":=").Qual(markersPath, "PackageMarkers").
						Call(jen.Id("ctx").Dot("Collector"), jen.Id("root")),
					ifErrNotNilReturnErr,
					jen.Id("markerValues").Op(":=").
						Id("markerSet").Index(jen.Id(markerDefName).Dot("Name")),
					jen.If(jen.Len(jen.Id("markerValues")).Op("==").Lit(0)).Block(jen.Continue()),
					jen.Comment("TODO: YOU CAN ALSO ADD YOUR CODE HERE"),
				),
				jen.Comment("TODO: OR HERE"),
				jen.Return(jen.Nil()),
			)

		filename := fmt.Sprintf("%s.go", strings.ToLower(g.name))
		if err := writeFile(f, g.path, filename); err != nil {
			return err
		}
	}

	return nil
}