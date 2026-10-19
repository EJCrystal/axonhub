package biz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRotatedAPIKeysMapsReplacedKeysByPosition(t *testing.T) {
	replacements := rotatedAPIKeys(
		[]string{"old-a", "shared", "old-b"},
		[]string{"new-a", "shared", "new-b"},
	)

	require.Equal(t, map[string]string{"old-a": "new-a", "old-b": "new-b"}, replacements)
}

func TestRotatedAPIKeysIgnoresReordering(t *testing.T) {
	// Both keys still exist, so this is a reorder. Reporting a rotation here
	// would rewrite a target onto a credential the operator did not choose.
	replacements := rotatedAPIKeys(
		[]string{"key-a", "key-b"},
		[]string{"key-b", "key-a"},
	)

	require.Empty(t, replacements)
}

func TestRotatedAPIKeysIgnoresAddedKeys(t *testing.T) {
	// Growing the key list only adds a credential: nothing was replaced.
	require.Empty(t, rotatedAPIKeys([]string{"key-a"}, []string{"key-a", "key-b"}))
	require.Empty(t, rotatedAPIKeys(nil, []string{"key-a"}))
}

func TestRotatedAPIKeysCollapsesMergedKeys(t *testing.T) {
	// Two account keys of one provider merged into one: both removed slots point
	// at the key that now owns that position, clamped to the new last slot.
	replacements := rotatedAPIKeys(
		[]string{"old-a", "old-b", "old-c"},
		[]string{"merged"},
	)

	require.Equal(t, map[string]string{
		"old-a": "merged",
		"old-b": "merged",
		"old-c": "merged",
	}, replacements)
}

func TestRotatedAPIKeysKeepsSurvivingKeyWhenShrinking(t *testing.T) {
	replacements := rotatedAPIKeys(
		[]string{"old-a", "kept"},
		[]string{"new-a", "kept"},
	)

	require.Equal(t, map[string]string{"old-a": "new-a"}, replacements)
}

func TestRotatedAPIKeysKeepsUnchangedSlots(t *testing.T) {
	replacements := rotatedAPIKeys(
		[]string{"key-a", "key-b"},
		[]string{"key-a", "new-b"},
	)

	require.Equal(t, map[string]string{"key-b": "new-b"}, replacements)
}

func TestRotatedAPIKeysSkipsBlankSlots(t *testing.T) {
	// The editor keeps a blank key row while a value is being typed, so an empty
	// slot must not rotate anything.
	replacements := rotatedAPIKeys(
		[]string{"key-a", ""},
		[]string{"new-a", ""},
	)

	require.Equal(t, map[string]string{"key-a": "new-a"}, replacements)
}
