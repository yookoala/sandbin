package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	configFileName    = ".sandbin.yaml"
	gitignoreFileName = ".gitignore"
)

// clientConfig is what .sandbin.yaml remembers about this project's sandbin setup.
// Only init writes an api_key into the file, at the user's explicit request; an
// ordinary upload run never newly persists a key that came from flag or env. The
// bin password stays env-only, as before.
type clientConfig struct {
	URL       string `yaml:"sandbin_url"`
	APIKey    string `yaml:"api_key"`
	Bin       string `yaml:"bin"`
	Protected bool   `yaml:"protected"`
}

// readConfig reads .sandbin.yaml from dir. found is false only when the file is
// simply not there: a file that is there but unreadable or unparsable is an error
// naming it, because falling through silently would make a second bin for one
// prototype.
func readConfig(dir string) (clientConfig, bool, error) {
	path := filepath.Join(dir, configFileName)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return clientConfig{}, false, nil
	}
	if err != nil {
		return clientConfig{}, false, fmt.Errorf("config file %s: %v", path, err)
	}

	var cfg clientConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return clientConfig{}, false, fmt.Errorf("config file %s: %v", path, err)
	}
	return cfg, true, nil
}

// writeConfig replaces .sandbin.yaml by renaming a temp file written in the same
// directory, so an interrupted run cannot leave half a config file.
func writeConfig(dir string, cfg clientConfig) error {
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	path := filepath.Join(dir, configFileName)
	tmp, err := os.CreateTemp(dir, configFileName+".tmp")
	if err != nil {
		return fmt.Errorf("config file %s: %v", path, err)
	}
	if err := writeAndClose(tmp, raw); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("config file %s: %v", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("config file %s: %v", path, err)
	}
	return nil
}

// ensureGitignore keeps the .gitignore beside the config listing configFileName: a
// missing file is created, a missing line is appended, and a file that already names
// the file is left byte for byte as the human wrote it. The .gitignore is the human's
// file, so it is edited in place rather than replaced behind a rename.
func ensureGitignore(dir string) error {
	path := filepath.Join(dir, gitignoreFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(configFileName+"\n"), 0644); err != nil {
			return fmt.Errorf("write %s: %v", path, err)
		}
		return nil
	}

	text := string(raw)
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == configFileName {
			return nil // the file already names the config file
		}
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n" // the appended line starts on a line of its own
	}
	if err := os.WriteFile(path, []byte(text+configFileName+"\n"), 0644); err != nil {
		return fmt.Errorf("append to %s: %v", path, err)
	}
	return nil
}

// writeAndClose fills a fresh temp file and leaves it 0600, the mode it is renamed
// in with: a bin hash and an api_key are not something other users of the machine
// should read.
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
