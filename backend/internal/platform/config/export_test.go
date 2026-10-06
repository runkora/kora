package config

// SetOverlayRoot points the loader at dir for the rest of a test binary's run.
func SetOverlayRoot(dir string) { overlayRoot = dir }
