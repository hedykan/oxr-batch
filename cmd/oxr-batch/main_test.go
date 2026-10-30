package main

import (
	"strings"
	"testing"
)

func TestRootHasCobraSubcommands(t *testing.T) {
	root := newRootCommand()
	for _, name := range []string{"serve", "convert"} {
		if command, _, err := root.Find([]string{name}); err != nil || command == nil || command.Name() != name {
			t.Fatalf("missing Cobra command %q", name)
		}
	}
}

func TestAPIModeRequiresKeyBeforeConversion(t *testing.T) {
	root := newRootCommand()
	root.SetArgs([]string{"convert", "--mode", "api", "book.pdf"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "API Key") {
		t.Fatalf("error = %v, want API Key validation", err)
	}
}
