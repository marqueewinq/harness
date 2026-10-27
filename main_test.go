package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// The review type is fixed at creation; only the dashboard may change it.
func TestReviewFlagOnlyOnCreation(t *testing.T) {
	for name, cmd := range map[string]*cobra.Command{
		"new":      newCmd(),
		"task new": taskNewCmd(),
		"epic new": epicNewCmd(),
	} {
		f := cmd.Flags().Lookup("review")
		if f == nil {
			t.Errorf("%s has no --review flag", name)
			continue
		}
		if f.DefValue != "default" {
			t.Errorf("%s --review default = %q, want default", name, f.DefValue)
		}
	}
	for name, cmd := range map[string]*cobra.Command{
		"task edit":  taskEditCmd(),
		"epic edit":  epicEditCmd(),
		"task start": taskStartCmd(),
	} {
		if cmd.Flags().Lookup("review") != nil {
			t.Errorf("%s must not accept --review", name)
		}
	}
}
