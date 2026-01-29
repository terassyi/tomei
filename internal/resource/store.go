package resource

// Store holds resources indexed by Kind and Name.
type Store struct {
	resources map[Kind]map[string]Resource
}

// NewStore creates a new Store.
func NewStore() *Store {
	return &Store{
		resources: make(map[Kind]map[string]Resource),
	}
}

// Add adds a resource to the store.
func (s *Store) Add(res Resource) {
	kind := res.Kind()
	if s.resources[kind] == nil {
		s.resources[kind] = make(map[string]Resource)
	}
	s.resources[kind][res.Name()] = res
}

// All returns all resources in the store.
func (s *Store) All() []Resource {
	var result []Resource
	for _, m := range s.resources {
		for _, res := range m {
			result = append(result, res)
		}
	}
	return result
}

// Get retrieves a typed resource by name.
// The Kind is inferred from the type parameter.
func Get[T Resource](s *Store, name string) (T, bool) {
	var zero T
	kind := zero.Kind()

	if m, ok := s.resources[kind]; ok {
		if res, ok := m[name]; ok {
			if typed, ok := res.(T); ok {
				return typed, true
			}
		}
	}
	return zero, false
}

// List returns all resources of a specific type.
// The Kind is inferred from the type parameter.
func List[T Resource](s *Store) []T {
	var zero T
	kind := zero.Kind()

	var result []T
	if m, ok := s.resources[kind]; ok {
		for _, res := range m {
			if typed, ok := res.(T); ok {
				result = append(result, typed)
			}
		}
	}
	return result
}
