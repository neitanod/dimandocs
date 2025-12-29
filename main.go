package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

var (
	// Version information (set via ldflags during build)
	Version   = "dev"
	BuildTime = "unknown"
)

func printUsage() {
	fmt.Fprintf(os.Stderr, `DimanDocs - A lightweight documentation browser for markdown files

USAGE:
    dimandocs [OPTIONS] [PATH]

PATH:
    If PATH is a directory: Browse all markdown files in that directory
    If PATH is a file:      Open browser directly to that file
    If PATH is omitted:     Use current directory or dimandocs.json config

OPTIONS:
    --config-file <file>    Path to configuration file (default: dimandocs.json if exists)
    --serve                 Start server without opening browser automatically
    --version               Show version information
    --help                  Show this help message

EXAMPLES:
    # Browse current directory with default settings
    dimandocs

    # Browse a specific directory
    dimandocs /path/to/docs

    # Open a specific markdown file
    dimandocs /path/to/README.md

    # Use custom config file
    dimandocs --config-file=custom.json

    # Start server without opening browser
    dimandocs --serve

    # Combine options
    dimandocs --serve --config-file=config.json /path/to/docs

CONFIGURATION:
    If dimandocs.json exists in the current directory, it will be used automatically.
    Otherwise, DimanDocs will use default settings (browse current directory for .md files).

    Default config when no dimandocs.json is found:
    - Browse current directory for .md files
    - Start server on port 8090
    - Automatically ignore common folders:
      * Version control: .git, .svn, .hg
      * Dependencies: node_modules, vendor, bower_components
      * Build outputs: build, dist, out, target
      * Frameworks: .next, .nuxt, .vuepress
      * Caches: .cache, __pycache__, .pytest_cache, .nyc_output
      * IDEs: .vscode, .idea, .eclipse
      * Python venvs: venv, env, .venv, .virtualenv
      * Coverage: coverage, htmlcov
      * Temp folders: tmp, temp, .tmp

For more information, visit: https://github.com/yourusername/dimandocs
`)
}

func main() {
	// Custom usage message
	flag.Usage = printUsage

	// Parse command line flags
	showVersion := flag.Bool("version", false, "Show version information")
	configFile := flag.String("config-file", "", "Path to configuration file (default: dimandocs.json if exists)")
	serveMode := flag.Bool("serve", false, "Start server without opening browser")
	flag.Parse()

	// Show version and exit
	if *showVersion {
		fmt.Printf("DimanDocs %s\n", Version)
		fmt.Printf("Build Time: %s\n", BuildTime)
		os.Exit(0)
	}

	// Get target path from first positional argument
	targetPath := ""
	if flag.NArg() > 0 {
		targetPath = flag.Arg(0)
	}

	// Create and initialize application
	app := NewApp()
	if err := app.Initialize(*configFile, targetPath); err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}

	// Start the server
	if err := app.Start(*serveMode); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}