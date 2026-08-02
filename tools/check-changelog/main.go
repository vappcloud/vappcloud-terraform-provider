package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	for _, name := range []string{"CHANGELOG.md", "LICENSE"} {
		info, err := os.Stat(name)
		if err != nil || info.Size() == 0 {
			fatalf("%s must exist and be non-empty", name)
		}
	}

	found := false
	err := filepath.WalkDir(".changelog", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" || entry.Name() == "README.md" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > 0 {
			found = true
		}
		return nil
	})
	if err != nil {
		fatalf("check .changelog: %v", err)
	}
	if !found {
		fatalf(".changelog must contain a non-empty Markdown fragment")
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
