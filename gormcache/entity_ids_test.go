package caches

import (
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

func TestExtractEntityIDs_NoSchema(t *testing.T) {
	db, _ := gorm.Open(tests.DummyDialector{}, &gorm.Config{})
	ids := extractEntityIDs(db)
	if ids != nil {
		t.Errorf("extractEntityIDs() = %v, want nil", ids)
	}
}

func TestExtractEntityIDs_InvalidReflectValue(t *testing.T) {
	db, _ := gorm.Open(tests.DummyDialector{}, &gorm.Config{})
	// ReflectValue is zero value by default (invalid)
	ids := extractEntityIDs(db)
	if ids != nil {
		t.Errorf("extractEntityIDs() = %v, want nil", ids)
	}
}

// TestExtractEntityIDs_PublicWrapperMatchesPrivate verifies that the exported
// ExtractEntityIDs delegates correctly to extractEntityIDs.
func TestExtractEntityIDs_PublicWrapperMatchesPrivate(t *testing.T) {
	db, _ := gorm.Open(tests.DummyDialector{}, &gorm.Config{})
	pub := ExtractEntityIDs(db)
	priv := extractEntityIDs(db)
	if pub != nil || priv != nil {
		t.Errorf("expected both nil, got public=%v private=%v", pub, priv)
	}
}
