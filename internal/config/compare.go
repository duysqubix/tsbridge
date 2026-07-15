// Package config handles configuration parsing and validation for tsbridge.
package config

import (
	"maps"
	"slices"

	"github.com/google/go-cmp/cmp"
)

// ServiceConfigEqual compares two service configurations and returns true if they are equal.
// This function is used to determine if a service needs to be restarted when configuration changes.
// It compares all fields that would require a service restart if changed.
func ServiceConfigEqual(a, b Service) bool {
	stringSliceComparer := cmp.Comparer(slices.Equal[[]string])

	tagsComparer := cmp.Comparer(func(x, y []string) bool {
		xCopy := slices.Clone(x)
		yCopy := slices.Clone(y)
		slices.Sort(xCopy)
		slices.Sort(yCopy)
		return slices.Equal(xCopy, yCopy)
	})

	stringMapComparer := cmp.Comparer(maps.Equal[map[string]string, map[string]string])

	opts := []cmp.Option{
		cmp.FilterPath(func(p cmp.Path) bool {
			return p.String() == "Tags"
		}, tagsComparer),
		cmp.FilterPath(func(p cmp.Path) bool {
			field := p.String()
			return field == "RemoveUpstream" || field == "RemoveDownstream"
		}, stringSliceComparer),
		cmp.FilterPath(func(p cmp.Path) bool {
			field := p.String()
			return field == "UpstreamHeaders" || field == "DownstreamHeaders"
		}, stringMapComparer),
	}

	return cmp.Equal(a, b, opts...)
}
