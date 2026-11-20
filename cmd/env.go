package cmd

import (
	"os"
	"strings"

	"github.com/kaizen-architecture/kaizen-mangal/color"
	"github.com/kaizen-architecture/kaizen-mangal/config"
	"github.com/kaizen-architecture/kaizen-mangal/constant"
	"github.com/kaizen-architecture/kaizen-mangal/style"
	"github.com/kaizen-architecture/kaizen-mangal/version"
	"github.com/kaizen-architecture/kaizen-mangal/where"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"golang.org/x/exp/slices"
)

func init() {
	rootCmd.AddCommand(envCmd)
	envCmd.Flags().BoolP("set-only", "s", false, "only show variables that are set")
	envCmd.Flags().BoolP("unset-only", "u", false, "only show variables that are unset")

	envCmd.MarkFlagsMutuallyExclusive("set-only", "unset-only")
}

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Show available environment variables",
	Long:  `Show available environment variables.`,
	Run: func(cmd *cobra.Command, args []string) {
		setOnly := lo.Must(cmd.Flags().GetBool("set-only"))
		unsetOnly := lo.Must(cmd.Flags().GetBool("unset-only"))

		extraEnvs := map[string]bool{
			where.EnvConfigPath:           true,
			where.EnvCachePath:            true,
			where.EnvSourcesPath:          true,
			version.EnvDisableUpdateCheck: true,
		}

		envs := make([]string, 0, len(config.EnvExposed)+len(extraEnvs))
		for _, env := range config.EnvExposed {
			envs = append(envs, strings.ToUpper(constant.EnvPrefix+"_"+config.EnvKeyReplacer.Replace(env)))
		}
		for env := range extraEnvs {
			envs = append(envs, env)
		}

		slices.Sort(envs)
		for _, env := range envs {
			value := os.Getenv(env)
			present := value != ""

			if setOnly || unsetOnly {
				if !present && setOnly {
					continue
				}

				if present && unsetOnly {
					continue
				}
			}

			cmd.Print(style.New().Bold(true).Foreground(color.Purple).Render(env))
			cmd.Print("=")

			if present {
				cmd.Println(style.Fg(color.Green)(value))
			} else {
				cmd.Println(style.Fg(color.Red)("unset"))
			}
		}
	},
}
