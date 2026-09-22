package routes

// mergePatch applies an RFC 7396 JSON merge patch to target and returns the
// result. Objects merge recursively, a null member deletes the key, and
// anything else replaces the value outright.
func mergePatch(target, patch any) any {
	p, ok := patch.(map[string]any)
	if !ok {
		return patch
	}

	t, ok := target.(map[string]any)
	if !ok || t == nil {
		t = map[string]any{}
	}

	for k, v := range p {
		if v == nil {
			delete(t, k)
		} else {
			t[k] = mergePatch(t[k], v)
		}
	}
	return t
}
