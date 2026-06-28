// Package config handles configuration parsing and validation for tsbridge.
package config

import (
	"maps"
	"slices"
	"time"

	"github.com/google/go-cmp/cmp"
)

// ServiceConfigEqual compares two service configurations and returns true if they are equal.
// This function is used to determine if a service needs to be restarted when configuration changes.
// It compares all fields that would require a service restart if changed.
func ServiceConfigEqual(a, b Service) bool {
	// Custom comparer for string slices where order matters.
	// slices.Equal treats nil and empty slices as equal.
	stringSliceComparer := cmp.Comparer(slices.Equal[[]string])

	// Custom comparer for the Tags field that ignores order.
	tagsComparer := cmp.Comparer(func(x, y []string) bool {
		xCopy := slices.Clone(x)
		yCopy := slices.Clone(y)
		slices.Sort(xCopy)
		slices.Sort(yCopy)
		return slices.Equal(xCopy, yCopy)
	})

	// Custom comparer for string maps.
	// maps.Equal treats nil and empty maps as equal.
	stringMapComparer := cmp.Comparer(maps.Equal[map[string]string, map[string]string])

	// Define comparison options
	opts := []cmp.Option{
		// Use custom comparer for Tags field (order doesn't matter)
		cmp.FilterPath(func(p cmp.Path) bool {
			return p.String() == "Tags"
		}, tagsComparer),
		// Use custom comparer for other string slice fields (order matters)
		cmp.FilterPath(func(p cmp.Path) bool {
			field := p.String()
			return field == "RemoveUpstream" || field == "RemoveDownstream"
		}, stringSliceComparer),
		// Use custom comparer for map fields
		cmp.FilterPath(func(p cmp.Path) bool {
			field := p.String()
			return field == "UpstreamHeaders" || field == "DownstreamHeaders"
		}, stringMapComparer),
	}

	// Use go-cmp to perform deep equality check with our options
	return cmp.Equal(a, b, opts...)
}

// durationPtrEqual compares two time.Duration pointers
func durationPtrEqual(a, b *time.Duration) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
