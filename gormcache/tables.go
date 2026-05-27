package caches

import "gorm.io/gorm"

func extractTables(db *gorm.DB) []string {
	seen := make(map[string]struct{})
	var tables []string

	if db.Statement.Table != "" {
		tables = append(tables, db.Statement.Table)
		seen[db.Statement.Table] = struct{}{}
	}

	if db.Statement.Schema != nil {
		for _, rel := range db.Statement.Schema.Relationships.Relations {
			tbl := rel.FieldSchema.Table
			if tbl == "" {
				continue
			}
			if _, exists := seen[tbl]; !exists {
				tables = append(tables, tbl)
				seen[tbl] = struct{}{}
			}
		}
	}

	return tables
}
