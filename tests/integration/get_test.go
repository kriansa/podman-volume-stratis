//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGet_NonExistent(t *testing.T) {
	_, err := testClient.Get("nonexistent-volume-12345")
	assert.Error(t, err, "get nonexistent volume should fail")
}
