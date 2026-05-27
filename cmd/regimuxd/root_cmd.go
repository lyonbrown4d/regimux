package main

import (
	"fmt"

	"github.com/lyonbrown4d/regimux/internal/build"
	"github.com/spf13/cobra"
)

func newRootCommand() *cobra.Command {
	var configPath string
	var showVersion bool

	cmd := &cobra.Command{
		Use:           "regimuxd",
		Short:         "Run the RegiMux registry proxy mirror gateway",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), build.VersionFromBuildInfo()); err != nil {
					return err
				}
				return nil
			}
			return run(cmd.Context(), configPath, args...)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "configs/regimux.hcl", "path to regimux HCL config file")
	cmd.Flags().BoolVar(&showVersion, "version", false, "print version and exit")
	cmd.Flags().ParseErrorsWhitelist.UnknownFlags = true
	return cmd
}
