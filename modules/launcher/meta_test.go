package launcher

import (
	"testing"

	"fyshos.com/tyde"
)

// TestModulesOwnMetadata checks that each module reports its own metadata
// (the settings enable and disable modules by it).
func TestModulesOwnMetadata(t *testing.T) {
	for _, meta := range []tyde.ModuleMetadata{calcMeta, qrMeta, largeTypeMeta, searchMeta, unytsMeta, urlMeta} {
		if got := meta.NewInstance().Metadata().Name; got != meta.Name {
			t.Errorf("module %q reports itself as %q", meta.Name, got)
		}
	}
}
