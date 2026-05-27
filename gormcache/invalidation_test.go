package caches

import "testing"

func TestMutationType_String(t *testing.T) {
	tests := []struct {
		mt   MutationType
		want string
	}{
		{MutationCreate, "create"},
		{MutationUpdate, "update"},
		{MutationDelete, "delete"},
		{MutationType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.mt.String(); got != tt.want {
			t.Errorf("MutationType(%d).String() = %q, want %q", tt.mt, got, tt.want)
		}
	}
}

func TestQueryTypeToMutationType(t *testing.T) {
	tests := []struct {
		qt   queryType
		want MutationType
	}{
		{uponCreate, MutationCreate},
		{uponUpdate, MutationUpdate},
		{uponDelete, MutationDelete},
		{uponQuery, MutationCreate}, // default fallback
	}
	for _, tt := range tests {
		if got := queryTypeToMutationType(tt.qt); got != tt.want {
			t.Errorf("queryTypeToMutationType(%d) = %v, want %v", tt.qt, got, tt.want)
		}
	}
}
