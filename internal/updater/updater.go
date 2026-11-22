package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultRepo        = "lirikrexhepi/Orchestrator"
	DefaultManifestURL = "https://github.com/" + DefaultRepo + "/releases/latest/download/latest.json"
	DefaultReleasesAPI = "https://api.github.com/repos/" + DefaultRepo + "/releases/latest"

	checkTimeout    = 15 * time.Second
	downloadTimeout = 10 * time.Minute
	maxManifestSize = 512 * 1024
	maxDownloadSize = 512 * 1024 * 1024
)

var (
	ErrDevBuild       = errors.New("automatic updates are disabled in development builds")
	ErrUnsupportedOS  = errors.New("automatic installer updates are currently supported on Windows only")
	ErrNoUpdate       = errors.New("already on the latest version")
	ErrMissingAsset   = errors.New("release does not contain an installer for this platform")
	ErrUntrustedHost  = errors.New("refusing to download update from untrusted host")
	ErrChecksumFailed = errors.New("downloaded installer failed SHA-256 verification")
)

// Manifest is published as `latest.json` on each GitHub Release.
type Manifest struct {
	Version   string                   `json:"version"`
	PubDate   string                   `json:"pubDate,omitempty"`
	Notes     string                   `json:"notes,omitempty"`
	Platforms map[string]PlatformAsset `json:"platforms"`
}

// PlatformAsset describes the downloadable installer for one OS/arch target.
type PlatformAsset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256,omitempty"`
}

// Status reports the outcome of checking for an Orchestrator app update.
type Status struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion,omitempty"`
	ReleaseNotes   string `json:"releaseNotes,omitempty"`
	PublishedAt    string `json:"publishedAt,omitempty"`
	DownloadURL    string `json:"downloadUrl,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
	Available      bool   `json:"available"`
	IsDevBuild     bool   `json:"isDevBuild"`
	Supported      bool   `json:"supported"`
	CheckedAt      int64  `json:"checkedAt"`
	Error          string `json:"error,omitempty"`
}

// Progress reports download/verify/install progress back to the UI.
type Progress struct {
	Stage      string `json:"stage"` // "downloading", "verifying", "installing", "error"
	Downloaded int64  `json:"downloaded,omitempty"`
	Total      int64  `json:"total,omitempty"`
	Percent    int    `json:"percent,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Client checks GitHub Releases for updates and downloads verified installers.
type Client struct {
	CurrentVersion     string
	ManifestURL        string
	ReleasesAPIURL     string
	Platform           string
	HTTPClient         *http.Client
	AllowInsecureLocal bool // unit tests only
}

// NewClient creates a production updater Client for the given embedded app version.
func NewClient(currentVersion string) *Client {
	return &Client{
		CurrentVersion: strings.TrimSpace(currentVersion),
		ManifestURL:    DefaultManifestURL,
		ReleasesAPIURL: DefaultReleasesAPI,
		Platform:       runtime.GOOS + "-" + runtime.GOARCH,
	}
}

func (c *Client) platformKey() string {
	if c.Platform != "" {
		return c.Platform
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

func (c *Client) isDev() bool {
	v := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(c.CurrentVersion), "v"))
	return v == "" || v == "dev" || strings.HasPrefix(v, "0.0.0")
}

func (c *Client) isSupportedPlatform() bool {
	return strings.HasPrefix(c.platformKey(), "windows-")
}

func (c *Client) httpClient(timeout time.Duration) *http.Client {
	base := c.HTTPClient
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many redirects")
			}
			if !c.isAllowedURL(req.URL) {
				return fmt.Errorf("%w: %s", ErrUntrustedHost, req.URL.Host)
			}
			return nil
		},
	}
	if base != nil {
		if base.Transport != nil {
			client.Transport = base.Transport
		}
	}
	return client
}

func (c *Client) isAllowedURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if c.AllowInsecureLocal && (host == "127.0.0.1" || host == "localhost") && (u.Scheme == "http" || u.Scheme == "https") {
		return true
	}
	if u.Scheme != "https" {
		return false
	}
	switch host {
	case "github.com", "api.github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return true
	default:
		return strings.HasSuffix(host, ".githubusercontent.com")
	}
}

// Check queries GitHub Releases (latest.json first, falling back to Releases API)
// and compares the published version against CurrentVersion.
func (c *Client) Check(ctx context.Context) Status {
	now := time.Now().UnixMilli()
	status := Status{
		CurrentVersion: normalizeVersionLabel(c.CurrentVersion),
		IsDevBuild:     c.isDev(),
		Supported:      c.isSupportedPlatform(),
		CheckedAt:      now,
	}

	manifest, err := c.fetchManifest(ctx)
	if err != nil {
		// Fall back to GitHub Releases API when latest.json isn't published yet.
		apiManifest, apiErr := c.fetchFromReleasesAPI(ctx)
		if apiErr != nil {
			status.Error = err.Error()
			return status
		}
		manifest = apiManifest
	}

	status.LatestVersion = normalizeVersionLabel(manifest.Version)
	status.ReleaseNotes = strings.TrimSpace(manifest.Notes)
	status.PublishedAt = strings.TrimSpace(manifest.PubDate)

	if asset, ok := manifest.Platforms[c.platformKey()]; ok && asset.URL != "" {
		status.DownloadURL = strings.TrimSpace(asset.URL)
		status.SHA256 = strings.TrimSpace(asset.SHA256)
	}

	if status.LatestVersion != "" && !status.IsDevBuild && status.DownloadURL != "" && status.Supported {
		status.Available = CompareVersions(status.LatestVersion, c.CurrentVersion) > 0
	}
	return status
}

func (c *Client) fetchManifest(ctx context.Context) (Manifest, error) {
	manifestURL := c.ManifestURL
	if manifestURL == "" {
		manifestURL = DefaultManifestURL
	}
	parsed, err := url.Parse(manifestURL)
	if err != nil || !c.isAllowedURL(parsed) {
		return Manifest{}, fmt.Errorf("%w: %s", ErrUntrustedHost, manifestURL)
	}

	reqCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return Manifest{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", "Orchestrator/"+normalizeVersionLabel(c.CurrentVersion))

	resp, err := c.httpClient(checkTimeout).Do(req)
	if err != nil {
		return Manifest{}, fmt.Errorf("check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return Manifest{}, errors.New("no published releases found yet")
	}
	if resp.StatusCode != http.StatusOK {
		return Manifest{}, fmt.Errorf("update check returned HTTP %d", resp.StatusCode)
	}

	var manifest Manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxManifestSize)).Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("invalid update manifest: %w", err)
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return Manifest{}, errors.New("update manifest missing version")
	}
	return manifest, nil
}

func (c *Client) fetchFromReleasesAPI(ctx context.Context) (Manifest, error) {
	apiURL := c.ReleasesAPIURL
	if apiURL == "" {
		apiURL = DefaultReleasesAPI
	}
	parsed, err := url.Parse(apiURL)
	if err != nil || !c.isAllowedURL(parsed) {
		return Manifest{}, fmt.Errorf("%w: %s", ErrUntrustedHost, apiURL)
	}

	reqCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, apiURL, nil)
	if err != nil {
		return Manifest{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Orchestrator/"+normalizeVersionLabel(c.CurrentVersion))

	resp, err := c.httpClient(checkTimeout).Do(req)
	if err != nil {
		return Manifest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Manifest{}, fmt.Errorf("github releases API returned HTTP %d", resp.StatusCode)
	}

	var release struct {
		TagName     string `json:"tag_name"`
		Body        string `json:"body"`
		PublishedAt string `json:"published_at"`
		Assets      []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxManifestSize)).Decode(&release); err != nil {
		return Manifest{}, err
	}
	if strings.TrimSpace(release.TagName) == "" {
		return Manifest{}, errors.New("latest release has no tag")
	}

	platforms := make(map[string]PlatformAsset)
	for _, a := range release.Assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, ".exe") && (strings.Contains(lower, "setup") || strings.Contains(lower, "installer")) {
			if strings.Contains(lower, "arm64") {
				platforms["windows-arm64"] = PlatformAsset{URL: a.BrowserDownloadURL}
			} else {
				platforms["windows-amd64"] = PlatformAsset{URL: a.BrowserDownloadURL}
			}
		}
	}
	return Manifest{
		Version:   release.TagName,
		PubDate:   release.PublishedAt,
		Notes:     release.Body,
		Platforms: platforms,
	}, nil
}

// DownloadAndVerify downloads the installer to a temp file, reports progress,
// and verifies SHA-256 (when provided in status.SHA256) before returning its path.
func (c *Client) DownloadAndVerify(ctx context.Context, status Status, onProgress func(Progress)) (string, error) {
	if !status.Supported {
		return "", ErrUnsupportedOS
	}
	if status.DownloadURL == "" {
		return "", ErrMissingAsset
	}
	parsed, err := url.Parse(status.DownloadURL)
	if err != nil || !c.isAllowedURL(parsed) {
		return "", fmt.Errorf("%w: %s", ErrUntrustedHost, status.DownloadURL)
	}

	emit := func(p Progress) {
		if onProgress != nil {
			onProgress(p)
		}
	}

	reqCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, status.DownloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Orchestrator/"+normalizeVersionLabel(c.CurrentVersion))

	resp, err := c.httpClient(downloadTimeout).Do(req)
	if err != nil {
		return "", fmt.Errorf("download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download update failed: HTTP %d", resp.StatusCode)
	}

	safeVersion := sanitizeFileToken(status.LatestVersion)
	if safeVersion == "" {
		safeVersion = "latest"
	}
	finalPath := filepath.Join(os.TempDir(), fmt.Sprintf("orchestrator-update-%s-setup.exe", safeVersion))
	partPath := finalPath + ".part"
	_ = os.Remove(partPath)

	out, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("create temp installer: %w", err)
	}

	hasher := sha256.New()
	total := resp.ContentLength
	var downloaded int64
	lastEmit := time.Time{}

	emit(Progress{Stage: "downloading", Downloaded: 0, Total: total, Percent: 0})

	buf := make([]byte, 32*1024)
	var copyErr error
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			downloaded += int64(n)
			if downloaded > maxDownloadSize {
				copyErr = errors.New("update installer exceeded maximum allowed size")
				break
			}
			if _, wErr := out.Write(buf[:n]); wErr != nil {
				copyErr = wErr
				break
			}
			_, _ = hasher.Write(buf[:n])

			now := time.Now()
			if now.Sub(lastEmit) >= 100*time.Millisecond {
				lastEmit = now
				pct := 0
				if total > 0 {
					pct = int(downloaded * 100 / total)
					if pct > 100 {
						pct = 100
					}
				}
				emit(Progress{Stage: "downloading", Downloaded: downloaded, Total: total, Percent: pct})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			copyErr = readErr
			break
		}
	}

	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(partPath)
		return "", fmt.Errorf("download update: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(partPath)
		return "", closeErr
	}

	emit(Progress{Stage: "verifying", Downloaded: downloaded, Total: total, Percent: 100})

	if expected := strings.ToLower(strings.TrimSpace(status.SHA256)); expected != "" {
		actual := hex.EncodeToString(hasher.Sum(nil))
		if actual != expected {
			_ = os.Remove(partPath)
			return "", fmt.Errorf("%w (expected %s, got %s)", ErrChecksumFailed, expected, actual)
		}
	}

	_ = os.Remove(finalPath)
	if err := os.Rename(partPath, finalPath); err != nil {
		_ = os.Remove(partPath)
		return "", fmt.Errorf("finalize temp installer: %w", err)
	}
	return finalPath, nil
}

// CleanupOldInstallers removes leftover Orchestrator setup executables and
// partial downloads from the system temp directory after a previous update.
func CleanupOldInstallers() {
	patterns := []string{
		filepath.Join(os.TempDir(), "orchestrator-update-*-setup.exe"),
		filepath.Join(os.TempDir(), "orchestrator-update-*-setup.exe.part"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			_ = os.Remove(path)
		}
	}
}

func normalizeVersionLabel(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "dev"
	}
	if strings.EqualFold(v, "dev") {
		return "dev"
	}
	return strings.TrimPrefix(v, "v")
}

func sanitizeFileToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type semver struct {
	major, minor, patch int
	pre                 string
	valid               bool
}

func parseSemver(raw string) semver {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if idx := strings.IndexByte(s, '+'); idx >= 0 {
		s = s[:idx]
	}
	var pre string
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		pre = s[idx+1:]
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return semver{}
	}
	maj, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	pat := 0
	var err3 error
	if len(parts) == 3 {
		pat, err3 = strconv.Atoi(parts[2])
	}
	if err1 != nil || err2 != nil || err3 != nil || maj < 0 || min < 0 || pat < 0 {
		return semver{}
	}
	return semver{major: maj, minor: min, patch: pat, pre: pre, valid: true}
}

// CompareVersions returns 1 if a > b, -1 if a < b, 0 if equal or incomparable.
func CompareVersions(a, b string) int {
	va := parseSemver(a)
	vb := parseSemver(b)
	if !va.valid || !vb.valid {
		return 0
	}
	if va.major != vb.major {
		return cmpInt(va.major, vb.major)
	}
	if va.minor != vb.minor {
		return cmpInt(va.minor, vb.minor)
	}
	if va.patch != vb.patch {
		return cmpInt(va.patch, vb.patch)
	}
	if va.pre == vb.pre {
		return 0
	}
	// Stable release > prerelease of same version.
	if va.pre == "" {
		return 1
	}
	if vb.pre == "" {
		return -1
	}
	return strings.Compare(va.pre, vb.pre)
}

func cmpInt(a, b int) int {
	if a > b {
		return 1
	}
	if a < b {
		return -1
	}
	return 0
}
