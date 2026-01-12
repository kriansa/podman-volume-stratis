//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapabilities_Basic(t *testing.T) {
	capability, err := testClient.Capabilities()
	require.NoError(t, err, "capabilities should succeed")

	// Stratis volumes should be local scope
	assert.Equal(t, "local", capability.Scope, "scope should be 'local'")
}
