package dnsforward

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/geo/geosite"
)

// geositeRule represents a geosite-based upstream routing rule.
type geositeRule struct {
	// category is the geosite category (e.g., "cn", "google").
	category string

	// upstreams is the list of upstream DNS servers for this category.
	upstreams []string
}

// geositeManager manages geosite data loading, querying, and updating.
type geositeManager struct {
	// db is the geosite database for domain lookups.
	db *geosite.Database

	// mu protects the manager state during updates.
	mu sync.RWMutex

	// logger is used for logging.
	logger *slog.Logger

	// dataSource is the source for geosite data (URL or file path).
	dataSource string

	// localFilePath is the path to the local geosite.dat file.
	localFilePath string

	// updateInterval is the interval for automatic updates.
	updateInterval time.Duration

	// stopCh is used to stop the auto-update goroutine.
	stopCh chan struct{}

	// httpClient is used for downloading geosite data.
	httpClient *http.Client

	// rules stores the geosite-based routing rules.
	rules []geositeRule

	// lastUpdate is the timestamp of the last successful update.
	lastUpdate time.Time
}

// Default geosite data URL (V2Ray format)
const defaultGeositeURL = "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat"

// newGeositeManager creates a new geosite manager with the given configuration.
func newGeositeManager(
	ctx context.Context,
	logger *slog.Logger,
	dataDir string,
	dataSource string,
	updateInterval time.Duration,
) (gm *geositeManager, err error) {
	// Use provided data directory or fall back to user home directory
	if dataDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("getting user home directory: %w", err)
		}
		dataDir = filepath.Join(homeDir, ".adguardhome", "data")
	}

	if err = os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data directory: %w", err)
	}

	localFilePath := filepath.Join(dataDir, "geosite.dat")

	gm = &geositeManager{
		logger:         logger,
		dataSource:     dataSource,
		localFilePath:  localFilePath,
		updateInterval: updateInterval,
		stopCh:         make(chan struct{}),
		httpClient:     &http.Client{Timeout: 5 * time.Minute},
	}

	// Load initial data
	err = gm.loadFromLocalOrRemote(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading geosite data: %w", err)
	}

	// Start auto-update if configured
	if gm.updateInterval > 0 {
		go gm.autoUpdate()
	}

	return gm, nil
}

// loadFromLocalOrRemote loads geosite data from local file if exists, otherwise downloads from remote.
func (gm *geositeManager) loadFromLocalOrRemote(ctx context.Context) (err error) {
	// Try to load from local file first
	if _, err = os.Stat(gm.localFilePath); err == nil {
		gm.logger.InfoContext(ctx, "loading geosite from local file", "path", gm.localFilePath)
		err = gm.loadFromFile(ctx, gm.localFilePath)
		if err == nil {
			return nil
		}
		gm.logger.WarnContext(ctx, "failed to load from local file, will download", "error", err)
	}

	// Download from remote
	return gm.downloadAndLoad(ctx)
}

// loadFromFile loads geosite data from a local file.
func (gm *geositeManager) loadFromFile(ctx context.Context, filePath string) (err error) {
	gm.logger.DebugContext(ctx, "loading geosite data from file", "path", filePath)

	db, err := geosite.FromFile(filePath)
	if err != nil {
		return fmt.Errorf("loading from file: %w", err)
	}

	gm.mu.Lock()
	// Replace the old database - the previous one is now unreferenced
	// and will be garbage collected.
	oldDB := gm.db
	gm.db = db
	fileInfo, _ := os.Stat(filePath)
	if fileInfo != nil {
		gm.lastUpdate = fileInfo.ModTime()
	}
	gm.mu.Unlock()

	// Help reclaim old database memory after releasing lock.
	// This is especially important for memory-mapped databases
	// which aren't released until GC runs.
	if oldDB != nil {
		runtime.GC()
	}

	gm.logger.InfoContext(ctx, "geosite data loaded successfully from file",
		"codes", db.CodeCount, "type", db.SourceType)

	return nil
}

// downloadAndLoad downloads geosite data from remote URL and saves to local file.
func (gm *geositeManager) downloadAndLoad(ctx context.Context) (err error) {
	// Determine download URL
	downloadURL := gm.dataSource
	if downloadURL == "" || !isURL(downloadURL) {
		downloadURL = defaultGeositeURL
	}

	gm.logger.InfoContext(ctx, "downloading geosite data", "url", downloadURL)

	// Download data
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := gm.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// Read data
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	// Save to local file
	err = os.WriteFile(gm.localFilePath, data, 0o644)
	if err != nil {
		return fmt.Errorf("saving to file: %w", err)
	}

	gm.logger.InfoContext(ctx, "geosite data downloaded and saved", "path", gm.localFilePath)

	// Load from the saved file
	return gm.loadFromFile(ctx, gm.localFilePath)
}

// getSites returns all geosite categories for the given domain.
// Returns an empty slice if the domain is not found in any category.
func (gm *geositeManager) getSites(domain string) (sites []string) {
	gm.mu.RLock()
	defer gm.mu.RUnlock()

	if gm.db == nil {
		return nil
	}

	codes := gm.db.LookupCodes(domain)

	return codes
}

// autoUpdate periodically updates geosite data from the configured source.
func (gm *geositeManager) autoUpdate() {
	ticker := time.NewTicker(gm.updateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctx := context.Background()
			gm.logger.InfoContext(ctx, "auto-updating geosite data")

			err := gm.downloadAndLoad(ctx)
			if err != nil {
				gm.logger.ErrorContext(ctx, "failed to auto-update geosite data", "error", err)
			} else {
				gm.logger.InfoContext(ctx, "geosite data auto-updated successfully")
			}

		case <-gm.stopCh:
			gm.logger.InfoContext(context.Background(), "stopping geosite auto-update")
			return
		}
	}
}

// update manually triggers a geosite data update.
func (gm *geositeManager) update(ctx context.Context) (err error) {
	return gm.downloadAndLoad(ctx)
}

// close stops the auto-update goroutine and cleans up resources.
func (gm *geositeManager) close() {
	close(gm.stopCh)
}

// isURL checks if the given string is an HTTP or HTTPS URL.
func isURL(s string) bool {
	return len(s) > 7 && (s[:7] == "http://" || s[:8] == "https://")
}

// parseGeositeRule parses a geosite rule line in the format:
// [geosite:category]upstream1 upstream2 ...
// Returns the category and upstreams, or empty strings if not a geosite rule.
func parseGeositeRule(line string) (category string, upstreams []string, ok bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[geosite:") {
		return "", nil, false
	}

	// Find the closing bracket.
	closeBracket := strings.Index(line, "]")
	if closeBracket == -1 || closeBracket < 10 {
		return "", nil, false
	}

	// Extract category (between "[geosite:" and "]").
	category = line[9:closeBracket]
	if category == "" {
		return "", nil, false
	}

	// Extract upstreams (after "]").
	upstreamsPart := strings.TrimSpace(line[closeBracket+1:])
	if upstreamsPart == "" {
		return "", nil, false
	}

	// Split upstreams by whitespace.
	upstreams = strings.Fields(upstreamsPart)

	return category, upstreams, true
}

// setRules sets the geosite routing rules from the upstream configuration.
func (gm *geositeManager) setRules(upstreams []string) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	gm.rules = nil

	for _, line := range upstreams {
		category, ups, ok := parseGeositeRule(line)
		if !ok {
			continue
		}

		gm.rules = append(gm.rules, geositeRule{
			category:  category,
			upstreams: ups,
		})

		gm.logger.DebugContext(
			context.Background(),
			"registered geosite rule",
			"category", category,
			"upstreams", ups,
		)
	}
}

// getUpstreamsForDomain returns the upstreams and matched tags for a domain
// based on its geosite categories. Returns empty strings and nil if no matching
// rule is found. Rules are matched in the order they appear in the configuration,
// so rules at the top of the config file have higher priority.
func (gm *geositeManager) getUpstreamsForDomain(domain string) (tags string, upstreams []string) {
	sites := gm.getSites(domain)

	if len(sites) == 0 {
		return "", nil
	}

	gm.mu.RLock()
	defer gm.mu.RUnlock()

	// Create a set of domain's categories for quick lookup
	siteSet := make(map[string]bool, len(sites))
	for _, site := range sites {
		siteSet[site] = true
	}

	// Iterate through rules in configuration order (top to bottom)
	// and find the first rule that matches any of the domain's categories
	var matchedTags []string
	var foundUpstreams []string

	for _, rule := range gm.rules {
		if siteSet[rule.category] {
			matchedTags = append(matchedTags, rule.category)
			if foundUpstreams == nil {
				foundUpstreams = rule.upstreams
			}
		}
	}

	if len(matchedTags) == 0 {
		return "", nil
	}

	return strings.Join(matchedTags, ","), foundUpstreams
}

// filterGeositeRules removes geosite rules from the upstream list and
// returns the filtered list.  The removed rules are stored in the manager.
func (gm *geositeManager) filterGeositeRules(upstreams []string) (filtered []string) {
	filtered = make([]string, 0, len(upstreams))

	for _, line := range upstreams {
		if _, _, ok := parseGeositeRule(line); ok {
			// This is a geosite rule, skip it from the regular upstream list.
			continue
		}
		filtered = append(filtered, line)
	}

	return filtered
}
