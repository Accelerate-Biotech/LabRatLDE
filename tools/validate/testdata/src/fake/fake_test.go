package fake

import "testing"

// SPEC-900-R01
func TestCitesR01(t *testing.T) {}

func TestCitesR01Inside(t *testing.T) {
	t.Run("sub", func(t *testing.T) {
		// SPEC-900-R01
	})
}

// SPEC-999-R01
func TestUnknown(t *testing.T) {}

// SPEC-900-R02
func helper() {}
