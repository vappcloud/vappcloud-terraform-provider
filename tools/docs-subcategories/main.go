package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	for _, directory := range []string{"docs/resources", "docs/data-sources"} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			panic(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				continue
			}
			path := filepath.Join(directory, entry.Name())
			contents, err := os.ReadFile(path)
			if err != nil {
				panic(err)
			}
			subcategory := "Data Sources"
			if directory == "docs/resources" {
				subcategory = "Managed Resources"
			}
			updated := strings.Replace(string(contents), `subcategory: ""`, `subcategory: "`+subcategory+`"`, 1)
			if updated == string(contents) && !strings.Contains(updated, `subcategory: "`+subcategory+`"`) {
				panic(fmt.Sprintf("%s has no generated subcategory field", path))
			}
			if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
				panic(err)
			}
		}
	}
}
