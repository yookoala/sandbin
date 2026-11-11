package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const stateFileName = ".sandbin.json"

// binState is what .sandbin.json remembers about this directory's bin. It never
// holds the API key or the bin password.
type binState struct {
	URL       string `json:"url"`
	Bin       string `json:"bin"`
	Protected bool   `json:"protected"`
}

// readState reads .sandbin.json from dir. found is false only when the file is
// simply not there: a file that is there but unreadable or unparsable is an error
// naming it, because falling through silently would make a second bin for one
// prototype.
func readState(dir string) (binState, bool, error) {
	path := filepath.Join(dir, stateFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return binState{}, false, nil
	}
	if err != nil {
		return binState{}, false, fmt.Errorf("state file %s: %v", path, err)
	}

	var state binState
	if err := json.Unmarshal(raw, &state); err != nil {
		return binState{}, false, fmt.Errorf("state file %s: %v", path, err)
	}
	if state.URL == "" || state.Bin == "" {
		return binState{}, false, fmt.Errorf("state file %s is not {\"url\": ..., \"bin\": ..., \"protected\": ...}", path)
	}
	return state, true, nil
}

// writeState replaces .sandbin.json by renaming a temp file written in the same
// directory, so an interrupted run cannot leave half a state file.
func writeState(dir string, state binState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	path := filepath.Join(dir, stateFileName)
	tmp, err := os.CreateTemp(dir, stateFileName+".tmp")
	if err != nil {
		return fmt.Errorf("state file %s: %v", path, err)
	}
	if err := writeAndClose(tmp, raw); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("state file %s: %v", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("state file %s: %v", path, err)
	}
	return nil
}

// writeAndClose fills a fresh temp file and leaves it 0600, the mode it is renamed
// in with: a bin hash is not something other users of the machine should read.
func writeAndClose(tmp *os.File, raw []byte) error {
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	return tmp.Close()
}
