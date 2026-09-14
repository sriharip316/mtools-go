package launch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const StartupFileName = ".mlaunch_startup"

// StateExists returns true if .mlaunch_startup exists in dir.
func StateExists(dir string) bool {
	path := filepath.Join(dir, StartupFileName)
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// LoadState reads and parses the .mlaunch_startup file from dir.
func LoadState(dir string) (*StartupState, error) {
	path := filepath.Join(dir, StartupFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no launch environment found in %s (or failed to read %s): %w", dir, StartupFileName, err)
	}

	var state StartupState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("corrupted %s file in %s: %w", StartupFileName, dir, err)
	}

	if state.StartupInfo == nil {
		state.StartupInfo = make(map[string]string)
	}
	if state.ParsedArgs == nil {
		state.ParsedArgs = make(map[string]any)
	}

	return &state, nil
}

// SaveState writes the StartupState to .mlaunch_startup in dir.
func SaveState(dir string, state *StartupState) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	path := filepath.Join(dir, StartupFileName)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize startup state: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}
