package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/doedja/hibachi-cli/internal/app"
	"github.com/doedja/hibachi-cli/internal/memory"
)

func newMemoryCmd() *cobra.Command {
	c := &cobra.Command{Use: "memory", Short: "AI memory (markdown files Claude reads and writes)"}
	c.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Print all memory files",
			RunE:  runMemoryShow,
		},
		&cobra.Command{
			Use:   "edit [file]",
			Short: "Open memory in $EDITOR",
			Args:  cobra.MaximumNArgs(1),
			RunE:  runMemoryEdit,
		},
		&cobra.Command{
			Use:   "rm [file]",
			Short: "Delete a memory file",
			Args:  cobra.ExactArgs(1),
			RunE:  runMemoryRm,
		},
		&cobra.Command{
			Use:   "path",
			Short: "Print the memory directory path",
			RunE:  runMemoryPath,
		},
	)
	return c
}

func runMemoryShow(cmd *cobra.Command, _ []string) error {
	a := app.From(cmd.Context())
	s, err := memory.Open(a.Cfg.Memory.Dir)
	if err != nil {
		return err
	}
	body, err := s.ReadAll()
	if err != nil {
		return err
	}
	if body == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "(memory is empty: %s)\n", s.Dir)
		return nil
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), body)
	return err
}

func runMemoryEdit(cmd *cobra.Command, args []string) error {
	a := app.From(cmd.Context())
	s, err := memory.Open(a.Cfg.Memory.Dir)
	if err != nil {
		return err
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	target := s.Dir
	if len(args) == 1 {
		name := args[0]
		if filepath.Ext(name) == "" {
			name += ".md"
		}
		target = filepath.Join(s.Dir, name)
	}
	ed := exec.Command(editor, target)
	ed.Stdin = os.Stdin
	ed.Stdout = os.Stdout
	ed.Stderr = os.Stderr
	return ed.Run()
}

func runMemoryRm(cmd *cobra.Command, args []string) error {
	a := app.From(cmd.Context())
	s, err := memory.Open(a.Cfg.Memory.Dir)
	if err != nil {
		return err
	}
	if err := s.Delete(args[0]); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "deleted %s\n", args[0])
	return nil
}

func runMemoryPath(cmd *cobra.Command, _ []string) error {
	a := app.From(cmd.Context())
	fmt.Fprintln(cmd.OutOrStdout(), a.Cfg.Memory.Dir)
	return nil
}
