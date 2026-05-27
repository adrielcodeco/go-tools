package caches

import (
	"reflect"

	"gorm.io/gorm"
)

func extractEntityIDs(db *gorm.DB) []interface{} {
	if db.Statement.Schema == nil {
		return nil
	}

	primaryFields := db.Statement.Schema.PrimaryFields
	if len(primaryFields) == 0 {
		return nil
	}

	rv := db.Statement.ReflectValue
	if !rv.IsValid() {
		return nil
	}

	var ids []interface{}

	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			elem := rv.Index(i)
			if elem.Kind() == reflect.Ptr {
				if elem.IsNil() {
					continue
				}
				elem = elem.Elem()
			}
			for _, pf := range primaryFields {
				fv := elem.FieldByName(pf.Name)
				if fv.IsValid() && !fv.IsZero() {
					ids = append(ids, fv.Interface())
				}
			}
		}
	case reflect.Struct:
		for _, pf := range primaryFields {
			fv := rv.FieldByName(pf.Name)
			if fv.IsValid() && !fv.IsZero() {
				ids = append(ids, fv.Interface())
			}
		}
	case reflect.Ptr:
		if !rv.IsNil() {
			elem := rv.Elem()
			if elem.Kind() == reflect.Struct {
				for _, pf := range primaryFields {
					fv := elem.FieldByName(pf.Name)
					if fv.IsValid() && !fv.IsZero() {
						ids = append(ids, fv.Interface())
					}
				}
			}
		}
	}

	if len(ids) == 0 {
		return nil
	}
	return ids
}
