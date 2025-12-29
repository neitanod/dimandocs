package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
)

// LoadConfig loads configuration from file and compiles regex patterns
func (a *App) LoadConfig(configFile string, targetPath string) error {
	if configFile == "" {
		configFile = "dimandocs.json"
	}

	// Check if config file exists
	data, err := ioutil.ReadFile(configFile)
	if err != nil {
		// If file doesn't exist and it's the default config, use default configuration
		if os.IsNotExist(err) && configFile == "dimandocs.json" {
			a.Config = getDefaultConfig()
		} else {
			return fmt.Errorf("failed to read config file: %w", err)
		}
	} else {
		// Config file exists, parse it
		if err := json.Unmarshal(data, &a.Config); err != nil {
			return fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	// Handle target path if provided
	if targetPath != "" {
		if err := a.handleTargetPath(targetPath); err != nil {
			return err
		}
	}

	// Compile ignore patterns
	for _, pattern := range a.Config.IgnorePatterns {
		regex, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("failed to compile ignore pattern '%s': %w", pattern, err)
		}
		a.IgnoreRegexes = append(a.IgnoreRegexes, regex)
	}

	// Compile file patterns for each directory
	a.FileRegexes = make(map[string]*regexp.Regexp)
	for _, dirConfig := range a.Config.Directories {
		pattern := dirConfig.FilePattern
		if pattern == "" {
			pattern = "^(?i)(readme\\.md)$" // Default to README.md files
		}
		regex, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("failed to compile file pattern '%s' for directory '%s': %w", pattern, dirConfig.Path, err)
		}
		a.FileRegexes[dirConfig.Path] = regex
	}

	return nil
}

// GetWorkingDirectory gets the current working directory
func GetWorkingDirectory() (string, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}
	return workingDir, nil
}

// getDefaultConfig returns the default configuration
func getDefaultConfig() Config {
	return Config{
		Directories: []DirectoryConfig{
			{
				Path:        "./",
				Name:        "Documents",
				FilePattern: "\\.md$",
			},
		},
		Port:  "8090",
		Title: "Documentation Browser",
		IgnorePatterns: []string{
			// Version control
			".*/\\.git(/.*)?$",
			".*/\\.svn(/.*)?$",
			".*/\\.hg(/.*)?$",

			// Dependencies
			".*/node_modules(/.*)?$",
			".*/vendor(/.*)?$",
			".*/bower_components(/.*)?$",

			// Build outputs
			".*/build(/.*)?$",
			".*/dist(/.*)?$",
			".*/out(/.*)?$",
			".*/target(/.*)?$",

			// Framework specific
			".*/\\.next(/.*)?$",
			".*/\\.nuxt(/.*)?$",
			".*/\\.vuepress(/.*)?$",

			// Caches
			".*/\\.cache(/.*)?$",
			".*/__pycache__(/.*)?$",
			".*/\\.pytest_cache(/.*)?$",
			".*/\\.nyc_output(/.*)?$",

			// IDEs
			".*/\\.vscode(/.*)?$",
			".*/\\.idea(/.*)?$",
			".*/\\.eclipse(/.*)?$",

			// Python virtual environments
			".*/venv(/.*)?$",
			".*/env(/.*)?$",
			".*/.venv(/.*)?$",
			".*/\\.virtualenv(/.*)?$",

			// Coverage and test outputs
			".*/coverage(/.*)?$",
			".*/htmlcov(/.*)?$",

			// Temporary files
			".*/tmp(/.*)?$",
			".*/temp(/.*)?$",
			".*/.tmp(/.*)?$",
		},
	}
}

// handleTargetPath processes the target path (file or directory)
func (a *App) handleTargetPath(targetPath string) error {
	// Get absolute path
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("failed to resolve path %s: %w", targetPath, err)
	}

	// Check if path exists
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("path does not exist: %s", targetPath)
	}

	if info.IsDir() {
		// It's a directory - override the config to browse this directory
		a.Config.Directories = []DirectoryConfig{
			{
				Path:        absPath,
				Name:        "Documents",
				FilePattern: "\\.md$",
			},
		}
	} else {
		// It's a file - store it to open directly in browser
		a.TargetFile = absPath

		// Set config to browse the directory containing the file
		dirPath := filepath.Dir(absPath)
		a.Config.Directories = []DirectoryConfig{
			{
				Path:        dirPath,
				Name:        "Documents",
				FilePattern: "\\.md$",
			},
		}
	}

	return nil
}