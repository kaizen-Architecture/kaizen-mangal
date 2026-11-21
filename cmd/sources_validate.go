package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/kaizen-architecture/kaizen-mangal/provider/custom"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
)

func init() {
	sourcesCmd.AddCommand(sourcesValidateCmd)
	sourcesValidateCmd.Flags().BoolP("json", "j", false, "output validation result as JSON")
	sourcesValidateCmd.SetOut(os.Stdout)
}

var sourcesValidateCmd = &cobra.Command{
	Use:   "validate <file.lua>",
	Short: "Validate a custom Lua source file without executing network or headless calls",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		res := custom.ValidateFile(args[0])
		if lo.Must(cmd.Flags().GetBool("json")) {
			b, err := json.Marshal(res)
			handleErr(err)
			cmd.Println(string(b))
			if !res.Valid {
				os.Exit(1)
			}
			return
		}

		if res.Valid {
			cmd.Printf("OK: %s (uses_headless=%v)\n", res.Name, res.UsesHeadless)
			return
		}

		cmd.Printf("INVALID: %s\n", res.Name)
		for _, e := range res.Errors {
			if e.Line > 0 {
				cmd.Printf("  [%s] line %d: %s\n", e.Kind, e.Line, e.Message)
			} else {
				cmd.Printf("  [%s] %s\n", e.Kind, e.Message)
			}
		}
		if len(res.MissingFunctions) > 0 {
			cmd.Printf("  missing functions: %s\n", strings.Join(res.MissingFunctions, ", "))
		}
		fmt.Fprintln(os.Stderr, "validation failed")
		os.Exit(1)
	},
}
