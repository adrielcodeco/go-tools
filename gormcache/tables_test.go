package caches

import (
	"reflect"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

func TestExtractTables_WithTable(t *testing.T) {
	db, _ := gorm.Open(tests.DummyDialector{}, &gorm.Config{})
	db.Statement.Table = "users"
	tables := extractTables(db)
	expected := []string{"users"}
	if !reflect.DeepEqual(tables, expected) {
		t.Errorf("extractTables() = %v, want %v", tables, expected)
	}
}

func TestExtractTables_NoTable(t *testing.T) {
	db, _ := gorm.Open(tests.DummyDialector{}, &gorm.Config{})
	tables := extractTables(db)
	if tables != nil {
		t.Errorf("extractTables() = %v, want nil", tables)
	}
}

func TestExtractTables_Dedup(t *testing.T) {
	db, _ := gorm.Open(tests.DummyDialector{}, &gorm.Config{})
	db.Statement.Table = "users"
	// Without schema relationships, we just verify no duplicates from the table itself
	tables := extractTables(db)
	if len(tables) != 1 || tables[0] != "users" {
		t.Errorf("extractTables() = %v, want [users]", tables)
	}
}
