package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

//go:embed templates/*
var templatesFS embed.FS

// Goldmark markdown renderer with GitHub Flavored Markdown support
var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM, // GitHub Flavored Markdown
	),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(), // Auto-generate heading IDs
	),
	goldmark.WithRendererOptions(
		html.WithUnsafe(), // Allow raw HTML in markdown
	),
)

// NewApp creates a new application instance
func NewApp() *App {
	return &App{
		FileRegexes: make(map[string]*regexp.Regexp),
	}
}

// Initialize sets up the application
func (a *App) Initialize(configFile string, targetPath string, useCache bool) error {
	// Get working directory
	workingDir, err := GetWorkingDirectory()
	if err != nil {
		return err
	}
	a.WorkingDir = workingDir
	a.UseCache = useCache

	// Load configuration
	if err := a.LoadConfig(configFile, targetPath); err != nil {
		return err
	}

	// Try to load from cache if enabled
	if a.UseCache {
		if err := a.loadFromCache(); err == nil {
			fmt.Printf("Loaded %d documents from cache\n", len(a.Documents))
			return nil
		}
		// If cache failed, continue with normal scan
		fmt.Println("Cache not found or invalid, scanning directories...")
	}

	// Scan directories for documents
	if err := a.ScanDirectories(); err != nil {
		return err
	}

	// Save to cache if enabled
	if a.UseCache {
		if err := a.saveToCache(); err != nil {
			log.Printf("Warning: failed to save cache: %v", err)
		} else {
			fmt.Printf("Saved %d documents to cache\n", len(a.Documents))
		}
	}

	return nil
}

// ScanDirectories scans all configured directories for documents
func (a *App) ScanDirectories() error {
	for _, dirConfig := range a.Config.Directories {
		if err := a.scanDirectory(dirConfig.Path, dirConfig.Name, a.FileRegexes[dirConfig.Path]); err != nil {
			return fmt.Errorf("failed to scan directory %s: %w", dirConfig.Path, err)
		}
	}
	return nil
}

// scanDirectory scans a single directory for matching files
func (a *App) scanDirectory(rootDir string, sourceName string, fileRegex *regexp.Regexp) error {
	return filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if a.shouldIgnorePath(path) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !info.IsDir() {
			filename := info.Name()
			if fileRegex.MatchString(filename) {
				if err := a.processFile(path, rootDir, sourceName); err != nil {
					log.Printf("Failed to process file %s: %v", path, err)
				}
			}
		}

		return nil
	})
}

// extractOverviewParagraph extracts the first paragraph after "## Overview" heading
func extractOverviewParagraph(content string) string {
	lines := strings.Split(content, "\n")
	foundOverview := false
	var paragraphLines []string

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Check if we found the Overview heading
		if strings.HasPrefix(trimmedLine, "## Overview") {
			foundOverview = true
			continue
		}

		// If we found Overview, start collecting paragraph lines
		if foundOverview {
			// Skip empty lines after the heading
			if trimmedLine == "" && len(paragraphLines) == 0 {
				continue
			}

			// Stop if we hit another heading or empty line after content
			if (strings.HasPrefix(trimmedLine, "#") || trimmedLine == "") && len(paragraphLines) > 0 {
				break
			}

			// Collect non-empty lines
			if trimmedLine != "" {
				paragraphLines = append(paragraphLines, trimmedLine)
			}
		}
	}

	return strings.Join(paragraphLines, " ")
}

// processFile processes a single markdown file
func (a *App) processFile(path, rootDir, sourceName string) error {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	relPath, _ := filepath.Rel(rootDir, path)
	dirName := filepath.Dir(relPath)
	if dirName == "." {
		dirName = "Root"
	}

	// Include filename in directory name
	filename := filepath.Base(path)
	if dirName == "Root" {
		dirName = filename
	} else {
		dirName = dirName + "/" + filename
	}

	absPath, _ := filepath.Abs(path)
	absDir := filepath.Dir(absPath)
	relAbsDir, _ := filepath.Rel(a.WorkingDir, absDir)

	// If path starts with ../, replace it with /
	if strings.HasPrefix(relAbsDir, "../") {
		relAbsDir = "/" + strings.TrimPrefix(relAbsDir, "../")
	}

	title := dirName
	if strings.Contains(string(content), "# ") {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "# ") {
				title = strings.TrimPrefix(line, "# ")
				break
			}
		}
	}

	// Extract overview paragraph
	overview := extractOverviewParagraph(string(content))

	doc := Document{
		Title:      title,
		Path:       path,
		Content:    string(content),
		RelPath:    relPath,
		DirName:    dirName,
		SourceDir:  rootDir,
		SourceName: sourceName,
		AbsPath:    relAbsDir,
		Overview:   overview,
	}

	a.Documents = append(a.Documents, doc)
	return nil
}

// shouldIgnorePath checks if a path should be ignored
func (a *App) shouldIgnorePath(path string) bool {
	for _, regex := range a.IgnoreRegexes {
		if regex.MatchString(path) {
			return true
		}
	}
	return false
}

// GroupDocumentsByDirectory groups documents by their source directory
func (a *App) GroupDocumentsByDirectory() []DirectoryGroup {
	groupMap := make(map[string][]Document)

	for _, doc := range a.Documents {
		groupMap[doc.SourceName] = append(groupMap[doc.SourceName], doc)
	}

	var groups []DirectoryGroup
	for name, docs := range groupMap {
		groups = append(groups, DirectoryGroup{
			Name:      name,
			Documents: docs,
		})
	}

	return groups
}

// BuildDirectoryTrees builds tree structures for each source directory
func (a *App) BuildDirectoryTrees() []DirectoryTree {
	// Group documents by source directory
	groupMap := make(map[string][]Document)
	for _, doc := range a.Documents {
		groupMap[doc.SourceName] = append(groupMap[doc.SourceName], doc)
	}

	var trees []DirectoryTree
	for sourceName, docs := range groupMap {
		root := &TreeNode{
			Name:     sourceName,
			Path:     "",
			IsFile:   false,
			Children: []*TreeNode{},
			IsOpen:   true,
		}

		// Build tree for each document
		for i := range docs {
			doc := &docs[i]
			addDocumentToTree(root, doc, doc.SourceDir)
		}

		trees = append(trees, DirectoryTree{
			Name: sourceName,
			Root: root,
		})
	}

	return trees
}

// addDocumentToTree adds a document to the tree structure
func addDocumentToTree(root *TreeNode, doc *Document, sourceDir string) {
	// Get relative path from source directory
	relPath, err := filepath.Rel(sourceDir, doc.Path)
	if err != nil {
		relPath = doc.Path
	}

	// Split path into parts
	parts := strings.Split(filepath.ToSlash(relPath), "/")

	current := root
	currentPath := ""

	// Navigate/create tree structure
	for i, part := range parts {
		if currentPath == "" {
			currentPath = part
		} else {
			currentPath = currentPath + "/" + part
		}

		isFile := (i == len(parts)-1)

		// Look for existing node
		var found *TreeNode
		for _, child := range current.Children {
			if child.Name == part {
				found = child
				break
			}
		}

		if found == nil {
			// Create new node
			newNode := &TreeNode{
				Name:   part,
				Path:   currentPath,
				IsFile: isFile,
				IsOpen: false,
			}

			if isFile {
				newNode.Document = doc
			}

			current.Children = append(current.Children, newNode)
			current = newNode
		} else {
			current = found
		}
	}
}

// SetupRoutes sets up HTTP routes
func (a *App) SetupRoutes() {
	http.HandleFunc("/", a.handleIndex)
	http.HandleFunc("/doc/", a.handleDocument)
	http.HandleFunc("/api/search", a.handleSearch)
	http.HandleFunc("/static/", a.handleStatic)
}

// handleIndex handles the index page
func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(templatesFS, "templates/index.html")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse template: %v", err), http.StatusInternalServerError)
		return
	}

	groups := a.GroupDocumentsByDirectory()
	trees := a.BuildDirectoryTrees()

	data := IndexData{
		Title:          a.Config.Title,
		Groups:         groups,
		Trees:          trees,
		TotalDocuments: len(a.Documents),
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to execute template: %v", err), http.StatusInternalServerError)
	}
}

// stripFrontmatter removes YAML frontmatter from markdown content
// Frontmatter is delimited by --- at the start and end
func stripFrontmatter(content string) string {
	// Check if content starts with frontmatter delimiter
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return content
	}

	// Find the closing delimiter
	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		return content
	}

	// Look for the second --- (closing delimiter)
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			// Found closing delimiter, return content after it
			return strings.Join(lines[i+1:], "\n")
		}
	}

	// No closing delimiter found, return original content
	return content
}

// handleDocument handles individual document pages
func (a *App) handleDocument(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/doc/")

	var docIndex = -1
	for i, d := range a.Documents {
		if d.RelPath == path {
			docIndex = i
			break
		}
	}

	if docIndex == -1 {
		http.NotFound(w, r)
		return
	}

	doc := &a.Documents[docIndex]

	// Load content on demand if not loaded yet
	if doc.Content == "" {
		content, err := ioutil.ReadFile(doc.Path)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to read document: %v", err), http.StatusInternalServerError)
			return
		}
		doc.Content = string(content)
	}

	tmpl, err := template.ParseFS(templatesFS, "templates/document.html")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse template: %v", err), http.StatusInternalServerError)
		return
	}

	// Remove YAML frontmatter if present
	content := stripFrontmatter(doc.Content)

	// Render markdown to HTML using Goldmark with GFM support
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(content), &buf); err != nil {
		http.Error(w, fmt.Sprintf("Failed to render markdown: %v", err), http.StatusInternalServerError)
		return
	}
	htmlContent := buf.Bytes()

	data := DocumentData{
		Title:    doc.Title,
		AppTitle: a.Config.Title,
		DirName:  doc.DirName,
		AbsPath:  doc.AbsPath,
		Content:  template.HTML(htmlContent),
	}

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("Failed to execute template: %v", err), http.StatusInternalServerError)
	}
}

// handleSearch handles search API requests
func (a *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	if query == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Document{})
		return
	}

	// Load all contents if not loaded yet (for search to work)
	if a.UseCache {
		for i := range a.Documents {
			if a.Documents[i].Content == "" {
				content, err := ioutil.ReadFile(a.Documents[i].Path)
				if err != nil {
					log.Printf("Warning: failed to read content for %s: %v", a.Documents[i].Path, err)
					continue
				}
				a.Documents[i].Content = string(content)
			}
		}
	}

	var results []Document
	for _, doc := range a.Documents {
		// Search in title, content, and overview (case-insensitive)
		if strings.Contains(strings.ToLower(doc.Title), query) ||
			strings.Contains(strings.ToLower(doc.Content), query) ||
			strings.Contains(strings.ToLower(doc.Overview), query) {
			results = append(results, doc)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(results); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode results: %v", err), http.StatusInternalServerError)
	}
}

// handleStatic handles static file serving
func (a *App) handleStatic(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, r.URL.Path[1:])
}

// findAvailablePort finds an available port starting from the given port
func findAvailablePort(startPort int) (int, error) {
	for port := startPort; port < startPort+100; port++ {
		addr := fmt.Sprintf(":%d", port)
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			listener.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available port found in range %d-%d", startPort, startPort+100)
}

// openBrowser opens the default browser with the given URL
func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}

// getFileURL finds the URL path for a specific file
func (a *App) getFileURL(targetFile string) (string, error) {
	// Find the document that matches the target file
	for _, doc := range a.Documents {
		absDocPath, err := filepath.Abs(doc.Path)
		if err != nil {
			continue
		}
		if absDocPath == targetFile {
			return "/doc/" + doc.RelPath, nil
		}
	}
	return "", fmt.Errorf("file not found in documents")
}

// Start starts the HTTP server
func (a *App) Start(serveMode bool) error {
	// Get desired port from config
	desiredPort := 8090
	if a.Config.Port != "" {
		if p, err := strconv.Atoi(a.Config.Port); err == nil {
			desiredPort = p
		}
	}

	// Find an available port
	port, err := findAvailablePort(desiredPort)
	if err != nil {
		return err
	}

	a.SetupRoutes()

	// Load document contents in background if using cache
	if a.UseCache {
		// Check if we need to load contents
		needsContentLoading := false
		for _, doc := range a.Documents {
			if doc.Content == "" {
				needsContentLoading = true
				break
			}
		}

		if needsContentLoading {
			go func() {
				fmt.Println("Loading document contents in background...")
				if err := a.loadDocumentContents(); err != nil {
					log.Printf("Warning: failed to load some document contents: %v", err)
				}
				fmt.Printf("Finished loading contents for %d documents\n", len(a.Documents))
			}()
		}
	}

	url := fmt.Sprintf("http://localhost:%d", port)

	// If a specific file was requested, find its URL path
	if a.TargetFile != "" {
		fileURL, err := a.getFileURL(a.TargetFile)
		if err != nil {
			log.Printf("Warning: could not find URL for file %s: %v\n", a.TargetFile, err)
		} else {
			url = fmt.Sprintf("http://localhost:%d%s", port, fileURL)
		}
	}

	fmt.Printf("\n")
	fmt.Printf("DimanDocs Server Started\n")
	fmt.Printf("========================\n")
	fmt.Printf("Found %d documents\n", len(a.Documents))
	fmt.Printf("Server running at: http://localhost:%d\n", port)
	if a.TargetFile != "" {
		fmt.Printf("Opening file: %s\n", a.TargetFile)
	}
	fmt.Printf("\n")
	fmt.Printf("Press Ctrl+C to stop the server\n")
	fmt.Printf("\n")

	// Open browser unless in serve mode
	if !serveMode {
		if err := openBrowser(url); err != nil {
			log.Printf("Could not open browser automatically: %v\n", err)
			fmt.Printf("Please open your browser manually to: %s\n", url)
		}
	}

	return http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
}

// loadFromCache loads documents from cache file (without content)
func (a *App) loadFromCache() error {
	cacheFile := ".dimandocs-cache.json"

	data, err := ioutil.ReadFile(cacheFile)
	if err != nil {
		return fmt.Errorf("failed to read cache file: %w", err)
	}

	var cache CacheData
	if err := json.Unmarshal(data, &cache); err != nil {
		return fmt.Errorf("failed to parse cache file: %w", err)
	}

	// Convert CachedDocuments to Documents (Content will be empty initially)
	a.Documents = make([]Document, len(cache.Documents))
	for i, cached := range cache.Documents {
		a.Documents[i] = Document{
			Title:      cached.Title,
			Path:       cached.Path,
			Content:    "", // Empty - will be loaded later
			RelPath:    cached.RelPath,
			DirName:    cached.DirName,
			SourceDir:  cached.SourceDir,
			SourceName: cached.SourceName,
			AbsPath:    cached.AbsPath,
			Overview:   cached.Overview,
		}
	}

	return nil
}

// loadDocumentContents loads the content of all documents from their files
func (a *App) loadDocumentContents() error {
	for i := range a.Documents {
		// Skip if content already loaded
		if a.Documents[i].Content != "" {
			continue
		}

		content, err := ioutil.ReadFile(a.Documents[i].Path)
		if err != nil {
			log.Printf("Warning: failed to read content for %s: %v", a.Documents[i].Path, err)
			continue
		}

		a.Documents[i].Content = string(content)
	}
	return nil
}

// saveToCache saves documents to cache file (without content)
func (a *App) saveToCache() error {
	cacheFile := ".dimandocs-cache.json"

	// Convert Documents to CachedDocuments (exclude Content field)
	cachedDocs := make([]CachedDocument, len(a.Documents))
	for i, doc := range a.Documents {
		cachedDocs[i] = CachedDocument{
			Title:      doc.Title,
			Path:       doc.Path,
			RelPath:    doc.RelPath,
			DirName:    doc.DirName,
			SourceDir:  doc.SourceDir,
			SourceName: doc.SourceName,
			AbsPath:    doc.AbsPath,
			Overview:   doc.Overview,
		}
	}

	cache := CacheData{
		Documents: cachedDocs,
		Version:   Version,
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %w", err)
	}

	if err := ioutil.WriteFile(cacheFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}