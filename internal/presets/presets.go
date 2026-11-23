// Package presets embeds curated lists of public, keyless RPC endpoints.
package presets

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed lists/*.txt
var lists embed.FS

// Names lists the available presets.
func Names() []string {
	entries, _ := fs.ReadDir(lists, "lists")
	var out []string
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".txt"))
	}
	sort.Strings(out)
	return out
}

// Get returns the endpoint list for a preset in the same format as an
// endpoints file.
func Get(name string) (string, error) {
	data, err := lists.ReadFile("lists/" + name + ".txt")
	if err != nil {
		return "", fmt.Errorf("unknown preset %q (available: %s)", name, strings.Join(Names(), ", "))
	}
	return string(data), nil
}
