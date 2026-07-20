// Package cmd wires the cobra subcommand tree for flightline.
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "flightline",
	Short: "App Store as Code: a declarative CLI for App Store Connect",
	Long: `Flightline turns App Store Connect into a structured, declarative surface:
read the entire account state, lint a desired-state YAML against it, preflight
every Apple rejection rule we know about, and apply changes idempotently,
so submissions stop being a clerical landmine.`,
	SilenceUsage: true,
	// main is the single printer: it redacts creds and maps ExitError to the exit code.
	SilenceErrors: true,
}

// Execute runs the root command and returns any error from the command tree.
func Execute() error {
	rejectUnknownSubcommands(rootCmd)
	return rootCmd.Execute()
}

// rejectUnknownSubcommands makes bare group commands fail on unmatched args.
// Without a RunE, cobra swallows a typo'd subcommand into help output and exits 0.
func rejectUnknownSubcommands(cmd *cobra.Command) {
	if cmd.HasSubCommands() && cmd.Run == nil && cmd.RunE == nil {
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown subcommand %q for %q — run '%s --help'", args[0], cmd.CommandPath(), cmd.CommandPath())
		}
	}
	for _, child := range cmd.Commands() {
		rejectUnknownSubcommands(child)
	}
}

// Root returns the fully-wired root command so generators can walk the command tree.
func Root() *cobra.Command {
	return rootCmd
}

// SetBuildInfo wires release metadata (injected via -ldflags at build time) into
// the --version output. Values are placeholders under `go run`/`go build` without ldflags.
func SetBuildInfo(version, commit, date string) {
	rootCmd.Version = version
	rootCmd.SetVersionTemplate(fmt.Sprintf("flightline %s (commit %s, built %s)\n", version, commit, date))
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().String("output", "table", "output format: table | json (report commands also accept tsv)")
	rootCmd.PersistentFlags().String("config", "", "config file (default $HOME/.config/flightline/config.yaml)")
	rootCmd.PersistentFlags().String("log-level", "info", "log level: debug | info | warn | error")
	rootCmd.PersistentFlags().Bool("no-color", false, "disable color output")
	rootCmd.PersistentFlags().String("key-id", "", "App Store Connect API key ID")
	rootCmd.PersistentFlags().String("issuer-id", "", "App Store Connect issuer ID")

	_ = viper.BindPFlag("output", rootCmd.PersistentFlags().Lookup("output"))
	_ = viper.BindPFlag("log_level", rootCmd.PersistentFlags().Lookup("log-level"))
	_ = viper.BindPFlag("no_color", rootCmd.PersistentFlags().Lookup("no-color"))
	_ = viper.BindPFlag("key_id", rootCmd.PersistentFlags().Lookup("key-id"))
	_ = viper.BindPFlag("issuer_id", rootCmd.PersistentFlags().Lookup("issuer-id"))
}

func initConfig() {
	viper.SetEnvPrefix("FLIGHTLINE")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	// App Store Connect creds via the same env names as the legacy `asc` tool,
	// so users can move to flightline without touching ~/.zshrc.
	_ = viper.BindEnv("key_id", "APP_STORE_CONNECT_KEY_ID", "FLIGHTLINE_KEY_ID")
	_ = viper.BindEnv("issuer_id", "APP_STORE_CONNECT_ISSUER_ID", "FLIGHTLINE_ISSUER_ID")
	_ = viper.BindEnv("vendor_number", "APP_STORE_CONNECT_VENDOR_NUMBER", "FLIGHTLINE_VENDOR_NUMBER")

	if cfg, _ := rootCmd.PersistentFlags().GetString("config"); cfg != "" {
		viper.SetConfigFile(cfg)
		if err := viper.ReadInConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "flightline: config file %s not readable: %v\n", cfg, err)
			os.Exit(1)
		}
		return
	}

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("$HOME/.config/flightline")
	_ = viper.ReadInConfig() // optional; absence is fine
}
