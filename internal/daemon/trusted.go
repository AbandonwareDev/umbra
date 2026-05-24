package daemon

import (
	"errors"
	"path/filepath"
	"strings"
)

// CheckTrustedPrefix validates that cmdPath resolves to a trusted path.
// It resolves symlinks via filepath.EvalSymlinks, then checks if the
// resolved path starts with any of the provided prefixes.
// An empty prefixes slice disables validation (all paths accepted).
func CheckTrustedPrefix(cmdPath string, prefixes []string) error {
	// Resolve symlinks first
	resolved, err := filepath.EvalSymlinks(cmdPath)
	if err != nil {
		return err
	}

	// Empty prefix list = disabled
	if len(prefixes) == 0 {
		return nil
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(resolved, prefix) {
			return nil
		}
	}

	return errors.New("command path " + resolved + " is not in trusted prefixes")
}
