package cli

// emptySlice ensures JSON encodes [] rather than null for typed empty slices.
func emptyTickets[T any](in []T) []T {
	if in == nil {
		return []T{}
	}
	return in
}
