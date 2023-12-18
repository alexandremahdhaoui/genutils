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

package genutils

//nolint:depguard
import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-tools/pkg/genall"
	"sigs.k8s.io/controller-tools/pkg/genall/help"
	prettyhelp "sigs.k8s.io/controller-tools/pkg/genall/help/pretty"
	"sigs.k8s.io/controller-tools/pkg/loader"
	"sigs.k8s.io/controller-tools/pkg/markers"
	"sigs.k8s.io/controller-tools/pkg/version"
)

type (
	Cmd struct {
		name        string
		description string
		helper      string

		// Options are specified to controller-gen by turning generators and output rules into markers, and then parsing them
		// using the standard registry logic (without the "+").
		// Each marker and output rule should thus be usable as a marker target.

		// generators maintains the list of all known generators, giving them names for use on the command line.
		// each turns into a command line option, and has options for output forms.
		generators map[string]genall.Generator

		// markerRegistry contains all the marker definitions used to process command line options.
		markerRegistry *markers.Registry

		// outputRules defines the list of all known output rules, giving them names for use on the command line.
		// Each output rule turns into two command line options:
		// - output:<generator>:<form> (per-generator output)
		// - output:<form> (default output)
		outputRules map[string]genall.OutputRule
	}

	Builder func() Cmd
)

func New(name string) Builder {
	return func() Cmd {
		return Cmd{
			name:           name,
			generators:     make(map[string]genall.Generator),
			markerRegistry: &markers.Registry{},
			outputRules: map[string]genall.OutputRule{
				"dir":    genall.OutputToDirectory(""),
				"stdout": genall.OutputToStdout,
			},
		}
	}
}

func (b Builder) WithDescription(description string) Builder {
	return func() Cmd {
		g := b()
		g.description = description

		return g
	}
}

func (b Builder) WithHelper(helper string) Builder {
	return func() Cmd {
		g := b()
		g.helper = helper

		return g
	}
}

func (b Builder) WithGenerator(key string, generator genall.Generator) Builder {
	return func() Cmd {
		g := b()
		g.generators[key] = generator

		return g
	}
}

func (b Builder) WithGenerators(generators map[string]genall.Generator) Builder {
	return func() Cmd {
		g := b()
		g.generators = generators

		return g
	}
}

func (b Builder) WithOutputRule(key string, outputRule genall.OutputRule) Builder {
	return func() Cmd {
		g := b()
		g.outputRules[key] = outputRule

		return g
	}
}

func (b Builder) WithOutputRules(outputRules map[string]genall.OutputRule) Builder {
	return func() Cmd {
		g := b()
		g.outputRules = outputRules

		return g
	}
}

func (b Builder) Apply() Cmd {
	return b()
}

func register(g Cmd) { //nolint:gochecknoinits,cyclop
	for genName, generator := range g.generators {
		// make the generator options marker itself
		def := markers.Must(markers.MakeDefinition(genName, markers.DescribesPackage, generator))
		if err := g.markerRegistry.Register(def); err != nil {
			panic(err)
		}

		if helpGiver, hasHelp := generator.(genall.HasHelp); hasHelp {
			if h := helpGiver.Help(); h != nil {
				g.markerRegistry.AddHelp(def, h)
			}
		}

		// make per-generation output rule markers
		for ruleName, rule := range g.outputRules {
			ruleMarker := markers.Must(markers.MakeDefinition(
				fmt.Sprintf("output:%s:%s", genName, ruleName), markers.DescribesPackage, rule))
			if err := g.markerRegistry.Register(ruleMarker); err != nil {
				panic(err)
			}

			if helpGiver, hasHelp := rule.(genall.HasHelp); hasHelp {
				if h := helpGiver.Help(); h != nil {
					g.markerRegistry.AddHelp(ruleMarker, h)
				}
			}
		}
	}

	// make "default output" output rule markers
	for ruleName, rule := range g.outputRules {
		ruleMarker := markers.Must(markers.MakeDefinition("output:"+ruleName, markers.DescribesPackage, rule))
		if err := g.markerRegistry.Register(ruleMarker); err != nil {
			panic(err)
		}

		if helpGiver, hasHelp := rule.(genall.HasHelp); hasHelp {
			if h := helpGiver.Help(); h != nil {
				g.markerRegistry.AddHelp(ruleMarker, h)
			}
		}
	}

	// add in the common options markers
	if err := genall.RegisterOptionsMarkers(g.markerRegistry); err != nil {
		panic(err)
	}
}

func (c Cmd) Run() {
	register(c)

	cmd := c.cmd()

	if err := cmd.Execute(); err != nil {
		var noUsageErr noUsageError
		if noUsage := errors.Is(err, noUsageErr); !noUsage {
			// print the usage unless we suppressed it
			if err := cmd.Usage(); err != nil {
				panic(err)
			}
		}

		_, err = fmt.Fprintf(
			cmd.OutOrStderr(),
			"run `%[1]s %[2]s -w` to see all available markers, or `%[1]s %[2]s -h` for usage\n",
			cmd.CalledAs(), strings.Join(os.Args[1:], " "))

		if err != nil {
			os.Exit(1)
		}

		os.Exit(1)
	}
}

//nolint:funlen
func (c Cmd) cmd() *cobra.Command {
	helpLevel := 0
	whichLevel := 0
	showVersion := false

	cmd := &cobra.Command{ //nolint:exhaustruct,exhaustivestruct
		Use:     c.name,
		Short:   c.description,
		Long:    c.description,
		Example: c.helper,
		RunE: func(ccmd *cobra.Command, rawOpts []string) error {
			// print version if asked for it
			if showVersion {
				version.Print()

				return nil
			}

			// print the help if we asked for it (since we've got a different help flag :-/), then bail
			if helpLevel > 0 {
				return ccmd.Usage()
			}

			// print the marker docs if we asked for them, then bail
			if whichLevel > 0 {
				return printMarkerDocs(c, ccmd, rawOpts, whichLevel)
			}

			// otherwise, set up the runtime for actually running the generators
			runtime, err := genall.FromOptions(c.markerRegistry, rawOpts)
			if err != nil {
				return err
			}

			if len(runtime.Generators) == 0 {
				return errors.New("no generators specified")
			}

			if hadErrs := runtime.Run(); hadErrs {
				// don't obscure the actual error with a bunch of usage
				return noUsageError{errors.New("not all generators ran successfully")}
			}

			return nil
		},
		SilenceUsage: true, // silence the usage, then print it out ourselves if it wasn't suppressed
	}

	cmd.Flags().CountVarP(&whichLevel, "which-markers", "w", "print out all markers available with the requested generators\n(up to -www for the most detailed output, or -wwww for json output)") //nolint:lll
	cmd.Flags().CountVarP(&helpLevel, "detailed-help", "h", "print out more detailed help\n(up to -hhh for the most detailed output, or -hhhh for json output)")                                   //nolint:lll
	cmd.Flags().BoolVar(&showVersion, "version", false, "show version")
	cmd.Flags().Bool("help", false, "print out usage and a summary of options")
	oldUsage := cmd.UsageFunc()
	cmd.SetUsageFunc(func(cmd *cobra.Command) error {
		if err := oldUsage(cmd); err != nil {
			return err
		}

		if helpLevel == 0 {
			helpLevel = summaryHelp
		}

		_, err := fmt.Fprintf(cmd.OutOrStderr(), "\n\nOptions\n\n")
		if err != nil {
			return err //nolint:wrapcheck
		}

		return helpForLevels(cmd.OutOrStdout(), cmd.OutOrStderr(), helpLevel, c.markerRegistry, help.SortByOption)
	})

	return cmd
}

// printMarkerDocs prints out marker help for the given generators specified in
// the rawOptions, at the given level.
func printMarkerDocs(g Cmd, cmd *cobra.Command, rawOptions []string, whichLevel int) error {
	// just grab a registry, so we don't lag while trying to load roots
	// (like we'd do if we just constructed the full runtime).
	reg, err := genall.RegistryFromOptions(g.markerRegistry, rawOptions)
	if err != nil {
		return err
	}

	return helpForLevels(cmd.OutOrStdout(), cmd.OutOrStderr(), whichLevel, reg, help.SortByCategory)
}

func helpForLevels(mainOut io.Writer, errOut io.Writer, whichLevel int, reg *markers.Registry, sorter help.SortGroup) error { //nolint:lll,cyclop
	helpInfo := help.ByCategory(reg, sorter)

	switch whichLevel {
	case jsonHelp:
		if err := json.NewEncoder(mainOut).Encode(helpInfo); err != nil {
			return err
		}
	case detailedHelp, fullHelp:
		fullDetail := whichLevel == fullHelp

		for _, cat := range helpInfo {
			if cat.Category == "" {
				continue
			}

			contents := prettyhelp.MarkersDetails(fullDetail, cat.Category, cat.Markers)
			if err := contents.WriteTo(errOut); err != nil {
				return err
			}
		}
	case summaryHelp:
		for _, cat := range helpInfo {
			if cat.Category == "" {
				continue
			}

			contents := prettyhelp.MarkersSummary(cat.Category, cat.Markers)
			if err := contents.WriteTo(errOut); err != nil {
				return err //nolint:wrapcheck
			}
		}
	}

	return nil
}

const (
	_ = iota
	summaryHelp
	detailedHelp
	fullHelp
	jsonHelp
)

// noUsageError suppresses usage printing when it occurs
// (since cobra doesn't provide a good way to avoid printing
// out usage in only certain situations).
type noUsageError struct{ error }

// WriteFile -----------------------------------------------------------------------------------------------------------

const headerTemplate = "%[2]s\n"

type WriteFileOption struct {
	// CmdName is the name of the command line used to
	CmdName    string
	Filename   string
	HeaderFile string

	Buffer *bytes.Buffer
	Ctx    *genall.GenerationContext
	Root   *loader.Package
}

func WriteFile(o WriteFileOption) error {
	var headerText string

	if o.HeaderFile != "" {
		headerBytes, err := o.Ctx.ReadFile(o.HeaderFile)
		if err != nil {
			return err
		}

		headerText = string(headerBytes)
	}

	buffer := new(bytes.Buffer)

	_, err := fmt.Fprintf(buffer, headerTemplate, o.Root.Name, headerText)
	if err != nil {
		return err //nolint:wrapcheck
	}

	if o.CmdName != "" {
		if _, err := fmt.Fprintf(buffer, "\n// Code generated by %s. DO NOT EDIT.\n", o.CmdName); err != nil {
			return err //nolint:wrapcheck
		}
	}

	buffer.Write(o.Buffer.Bytes())

	outBytes := buffer.Bytes()
	if formatted, err := format.Source(outBytes); err != nil {
		o.Root.AddError(err)
	} else {
		outBytes = formatted
	}

	outputFile, err := o.Ctx.Open(o.Root, o.Filename)
	if err != nil {
		return err //nolint:wrapcheck
	}

	defer func(outputFile io.WriteCloser) {
		err := outputFile.Close()
		if err != nil {
			o.Root.AddError(err)
		}
	}(outputFile)

	n, err := outputFile.Write(outBytes)
	if err != nil {
		return err //nolint:wrapcheck
	}

	if n < len(outBytes) {
		return io.ErrShortWrite
	}

	return nil
}

// Other Utils  --------------------------------------------------------------------------------------------------------

func Title(s string) string {
	r := []rune(s)

	return string(append([]rune{unicode.ToUpper(r[0])}, r[1:]...))
}

func GeneratedFilename(prefix, name string) string {
	return fmt.Sprintf("zz_generated.%s.%s.go", prefix, name)
}
