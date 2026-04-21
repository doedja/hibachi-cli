package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

const Version = "0.1.0-dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("hibachi-cli", Version)
			return nil
		},
	}
}
