package fp

func Map[T any, R any](in []T, fn func(T) R) []R {
	out := make([]R, len(in))
	for i, v := range in {
		out[i] = fn(v)
	}
	return out
}

func Remove[T comparable](in []T, v T) []T {
	for i, item := range in {
		if item == v {
			return append(in[:i], in[i+1:]...)
		}
	}
	return in
}

func Dedup[T comparable](in []T) []T {
	seen := make(map[T]struct{})
	out := make([]T, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

// PtrEq reports whether both pointers are nil or point to equal values.
func PtrEq[T comparable](a, b *T) bool {
	return a == b || (a != nil && b != nil && *a == *b)
}
