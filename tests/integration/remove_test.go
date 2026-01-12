//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemove_NonExistent(t *testing.T) {
	err := testClient.Remove("nonexistent-volume-12345")
	assert.Error(t, err, "remove nonexistent volume should fail")
}
