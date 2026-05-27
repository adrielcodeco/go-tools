package caches

// MutationType represents the type of mutation that triggered cache invalidation.
type MutationType int

const (
	MutationCreate MutationType = iota
	MutationUpdate
	MutationDelete
)

func (m MutationType) String() string {
	switch m {
	case MutationCreate:
		return "create"
	case MutationUpdate:
		return "update"
	case MutationDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// InvalidationEvent contains metadata about a mutation that triggered cache invalidation.
type InvalidationEvent struct {
	Tables       []string
	EntityIDs    []interface{}
	MutationType MutationType
	Tags         []string
}

func queryTypeToMutationType(qt queryType) MutationType {
	switch qt {
	case uponCreate:
		return MutationCreate
	case uponUpdate:
		return MutationUpdate
	case uponDelete:
		return MutationDelete
	default:
		return MutationCreate
	}
}
