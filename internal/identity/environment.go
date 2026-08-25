package identity

// EnvironmentIdentity binds the executable environment selector to its
// content-addressed descriptor. The URI is an input selector, not a path that
// is written separately into the normalized Manifest projection.
func EnvironmentIdentity(uri, descriptorHash string) (string, error) {
	return HashCanonical(map[string]any{
		"uri":             uri,
		"descriptor_hash": descriptorHash,
	})
}
