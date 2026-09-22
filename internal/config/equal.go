package config

// serversEqual reports whether a and b describe the same server
// semantically: field order in Env doesn't matter, and Headers is
// already unordered as a map.
func serversEqual(a, b Server) bool {
	if a.Name != b.Name || a.Type != b.Type {
		return false
	}
	switch a.Type {
	case ServerTypeStdio:
		return a.Command == b.Command &&
			stringSlicesEqual(a.Args, b.Args) &&
			envVarsEqual(a.Env, b.Env)
	case ServerTypeRemote:
		return a.URL == b.URL &&
			a.Transport == b.Transport &&
			a.BearerTokenEnvVar == b.BearerTokenEnvVar &&
			headersEqual(a.Headers, b.Headers)
	default:
		return false
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func envVarsEqual(a, b []EnvVar) bool {
	if len(a) != len(b) {
		return false
	}
	am := make(map[string]EnvVar, len(a))
	for _, ev := range a {
		am[ev.Name] = ev
	}
	for _, ev := range b {
		other, ok := am[ev.Name]
		if !ok || !envVarEqual(ev, other) {
			return false
		}
	}
	return true
}

func envVarEqual(a, b EnvVar) bool {
	if a.Kind != b.Kind || a.Value != b.Value {
		return false
	}
	if (a.Default == nil) != (b.Default == nil) {
		return false
	}
	return a.Default == nil || *a.Default == *b.Default
}

func headersEqual(a, b map[string]HeaderValue) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		other, ok := b[k]
		if !ok || v != other {
			return false
		}
	}
	return true
}
