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
