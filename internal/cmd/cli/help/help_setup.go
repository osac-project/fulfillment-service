/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package help

import (
	"bytes"
	"embed"
	"log/slog"
	"os"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"github.com/osac-project/fulfillment-service/internal/templating"
)

//go:embed templates
var templatesFS embed.FS

// Setup configures the given command and all its subcommands to render their help output as styled Markdown.
func Setup(cmd *cobra.Command) {
	// Create a silent logger for the templating engine, as help rendering happens before the persistent pre-run
	// hook sets up a proper logger, so we discard log output here.
	logger := slog.New(slog.DiscardHandler)

	// Build the templating engine from the embedded templates directory:
	engine, err := templating.NewEngine().
		SetLogger(logger).
		AddFS(templatesFS).
		SetDir("templates").
		AddFunction("flags", flagsFunc).
		Build()
	if err != nil {
		return
	}

	// Select the style according to the terminal color scheme, and also prepare a colorless style for when color
	// isn't wanted (see useColor below) -- we still want Markdown structure (headings, code spans, etc.) to be
	// rendered properly, just without ANSI color codes.
	var style ansi.StyleConfig
	if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		style = styles.DarkStyleConfig
	} else {
		style = styles.LightStyleConfig
	}
	plainStyle := styles.ASCIIStyleConfig

	// Regardless of the style, we want to remove the default document margin and leading newline, so the output is
	// flush with the left edge of the terminal. We also don't want to display the heading prefixes, and we don't
	// want code inside paragraphs to change the background color or add prefixes and suffixes. Apply these tweaks
	// to both styles so the plain (no-color) rendering looks as clean as the colored one.
	for _, s := range []*ansi.StyleConfig{&style, &plainStyle} {
		zero := new(uint)
		s.Document.Margin = zero
		s.Document.BlockPrefix = ""
		s.H2.Prefix = ""
		s.H3.Prefix = ""
		s.H4.Prefix = ""
		s.H5.Prefix = ""
		s.H6.Prefix = ""
		s.Code.BackgroundColor = nil
		s.Code.Prefix = ""
		s.Code.Suffix = ""
	}

	// Set the help function for the command and all its subcommands. The renderer is created each time the
	// help is displayed, so that it can adapt to the current terminal width.
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		// If the output is a terminal, we want to adjust the width of the terminal, but never more than the
		// maximun width that we consider readable:
		out := c.OutOrStdout()
		var width int
		if file, ok := out.(*os.File); ok {
			fd := int(file.Fd())
			if term.IsTerminal(fd) {
				width, _, err = term.GetSize(fd)
				if err != nil {
					c.PrintErrln("Error getting terminal size:", err)
					return
				}
			}
		}
		width = min(width, maxReadableWidth)

		// Color is opt-in: default is plain/no-color, even in an interactive terminal. Set FORCE_COLOR to enable
		// styled output; NO_COLOR always wins if both are set.
		useColor := os.Getenv("FORCE_COLOR") != "" && os.Getenv("NO_COLOR") == ""

		// Render the help output:
		var buffer bytes.Buffer
		err = engine.Execute(&buffer, "command_help.md", c)
		if err != nil {
			c.PrintErrln("Error executing help template:", err)
			return
		}
		rendererOpts := []glamour.TermRendererOption{glamour.WithWordWrap(width)}
		if useColor {
			rendererOpts = append(rendererOpts, glamour.WithStyles(style))
		} else {
			rendererOpts = append(rendererOpts, glamour.WithStyles(plainStyle))
		}
		renderer, err := glamour.NewTermRenderer(rendererOpts...)
		if err != nil {
			c.PrintErrln("Error creating renderer:", err)
			return
		}
		text, err := renderer.Render(buffer.String())
		if err != nil {
			c.Print(buffer.String())
			return
		}
		_, err = lipgloss.Fprint(out, text)
		if err != nil {
			c.PrintErrln("Error writing help output:", err)
			return
		}
	})
}

// flagsFunc converts a pflag.FlagSet into a slice of visible flags, excluding hidden flags and the
// built-in help flag.
func flagsFunc(fs *pflag.FlagSet) []*pflag.Flag {
	var result []*pflag.Flag
	fs.VisitAll(func(f *pflag.Flag) {
		if !f.Hidden && f.Name != "help" {
			result = append(result, f)
		}
	})
	return result
}

// maxReadableWidth is the maximum width for help output that we consider readable.
const maxReadableWidth = 100
