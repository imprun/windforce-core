package state

import (
	"sort"
	"strings"
)

func normalizeResourceTypeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "1"
	}
	return version
}

func resourceTypeKey(name string, version string) string {
	return strings.TrimSpace(name) + "@" + normalizeResourceTypeVersion(version)
}

func sortResourceTypes(items []ResourceType) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].Version < items[j].Version
		}
		return items[i].Name < items[j].Name
	})
}
