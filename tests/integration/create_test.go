//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreate_InvalidName_Empty(t *testing.T) {
	err := testClient.Create("", nil)
	assert.Error(t, err, "create with empty name should fail")
}

func TestCreate_InvalidName_TooLong(t *testing.T) {
	name := strings.Repeat("a", 66) // MaxNameLength is 65
	err := testClient.Create(name, nil)
	assert.Error(t, err, "create with name exceeding 65 chars should fail")
}

func TestCreate_InvalidName_SingleChar(t *testing.T) {
	err := testClient.Create("a", nil)
	assert.Error(t, err, "single character name should fail (min is 2)")
}

func TestCreate_InvalidName_SpecialChars(t *testing.T) {
	invalidNames := []string{
		"test/volume",
		"test:volume",
		"test\\volume",
		"../test",
	}

	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			err := testClient.Create(name, nil)
			assert.Error(t, err, "create with invalid name %q should fail", name)
		})
	}
}

func TestCreate_Duplicate(t *testing.T) {
	name := uniqueVolumeName(t)
	cleanupVolume(t, name)

	// Create first volume
	err := testClient.Create(name, nil)
	require.NoError(t, err, "first create should succeed")

	// Try to create duplicate - driver returns error for duplicates
	err = testClient.Create(name, nil)
	assert.Error(t, err, "duplicate create should fail")
}

func TestCreate_ValidName_MinLength(t *testing.T) {
	name := "ab" // Exactly 2 chars (minimum)
	cleanupVolume(t, name)
	err := testClient.Create(name, nil)
	require.NoError(t, err, "2 char name should succeed")
	_ = testClient.Remove(name)
}

func TestCreate_ValidName_MaxLength(t *testing.T) {
	name := strings.Repeat("a", 65) // Exactly MaxNameLength
	cleanupVolume(t, name)
	err := testClient.Create(name, nil)
	require.NoError(t, err, "65 char name should succeed")
	_ = testClient.Remove(name)
}
