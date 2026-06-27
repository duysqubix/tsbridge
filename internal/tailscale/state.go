package tailscale

import (
	"os"
	"path/filepath"
)

// hasExistingState checks if a service has existing tsnet state
func hasExistingState(stateDir, serviceName string) bool {
	stateFile := filepath.Join(stateDir, serviceName, "tailscaled.state")
	_, err := os.Stat(stateFile)
	return err == nil
}
