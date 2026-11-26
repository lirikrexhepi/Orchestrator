package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.1", "1.0.0", 1},
		{"v1.10.0", "v1.2.0", 1},
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.0-beta.1", 1},
		{"1.0.0-beta.1", "1.0.0", -1},
		{"1.0.0-beta.2", "1.0.0-beta.1", 1},
		{"0.9.9", "1.0.0", -1},
		{"dev", "1.0.0", 0},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCheckAndDownloadVerify(t *testing.T) {
	payload := []byte("fake-nsis-installer-bytes-v1.1.0")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest.json":
			_ = json.NewEncoder(w).Encode(Manifest{
				Version: "1.1.0",
				PubDate: "2026-11-22T16:00:00Z",
				Notes:   "Bug fixes and faster startup.",
				Platforms: map[string]PlatformAsset{
					"windows-amd64": {
						URL:    srv.URL + "/Orchestrator-1.1.0-amd64-setup.exe",
						SHA256: hexSum,
					},
				},
			})
		case "/Orchestrator-1.1.0-amd64-setup.exe":
			w.Header().Set("Content-Length", "32")
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := &Client{
		CurrentVersion:     "1.0.0",
		ManifestURL:        srv.URL + "/latest.json",
		Platform:           "windows-amd64",
		AllowInsecureLocal: true,
	}

	status := client.Check(context.Background())
	if status.Error != "" {
		t.Fatalf("unexpected check error: %s", status.Error)
	}
	if !status.Available {
		t.Fatalf("expected update to be available: %+v", status)
	}
	if status.LatestVersion != "1.1.0" {
		t.Fatalf("LatestVersion = %q, want 1.1.0", status.LatestVersion)
	}

	var stages []string
	path, err := client.DownloadAndVerify(context.Background(), status, func(p Progress) {
		stages = append(stages, p.Stage)
	})
	if err != nil {
		t.Fatalf("DownloadAndVerify failed: %v", err)
	}
	defer os.Remove(path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded installer: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("downloaded payload mismatch")
	}
	if len(stages) < 2 || stages[len(stages)-1] != "verifying" {
		t.Fatalf("unexpected progress stages: %v", stages)
	}

	// Bad checksum must fail and clean up
	badStatus := status
	badStatus.SHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := client.DownloadAndVerify(context.Background(), badStatus, nil); !errors.Is(err, ErrChecksumFailed) {
		t.Fatalf("expected ErrChecksumFailed, got %v", err)
	}
}

func TestRejectsUntrustedHost(t *testing.T) {
	client := NewClient("1.0.0")
	status := Status{
		LatestVersion: "1.1.0",
		DownloadURL:   "https://evil.example.com/Orchestrator-setup.exe",
		Supported:     true,
	}
	if _, err := client.DownloadAndVerify(context.Background(), status, nil); !errors.Is(err, ErrUntrustedHost) {
		t.Fatalf("expected ErrUntrustedHost, got %v", err)
	}
}

func TestDevBuildDoesNotFlagAutoUpdate(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Manifest{
			Version: "1.2.0",
			Platforms: map[string]PlatformAsset{
				"windows-amd64": {URL: srv.URL + "/setup.exe"},
			},
		})
	}))
	defer srv.Close()

	client := &Client{
		CurrentVersion:     "dev",
		ManifestURL:        srv.URL + "/latest.json",
		Platform:           "windows-amd64",
		AllowInsecureLocal: true,
	}
	status := client.Check(context.Background())
	if !status.IsDevBuild {
		t.Fatalf("expected IsDevBuild=true")
	}
	if status.Available {
		t.Fatalf("dev builds must not advertise automatic in-place update")
	}
	if status.LatestVersion != "1.2.0" {
		t.Fatalf("LatestVersion = %q, want 1.2.0", status.LatestVersion)
	}
}

func TestFallbackToReleasesAPIWhenManifestIsStale(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest.json":
			// Stale CDN response returning current version
			_ = json.NewEncoder(w).Encode(Manifest{
				Version: "1.0.0",
				Platforms: map[string]PlatformAsset{
					"windows-amd64": {URL: srv.URL + "/setup-1.0.0.exe"},
				},
			})
		case "/api/releases/latest":
			// Live GitHub API returning newly published release
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name":     "v1.0.1",
				"body":         "Fresh fixes",
				"published_at": "2026-11-26T19:00:00Z",
				"assets": []map[string]any{
					{
						"name":                 "Orchestrator-1.0.1-setup.exe",
						"browser_download_url": srv.URL + "/setup-1.0.1.exe",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := &Client{
		CurrentVersion:     "1.0.0",
		ManifestURL:        srv.URL + "/latest.json",
		ReleasesAPIURL:     srv.URL + "/api/releases/latest",
		Platform:           "windows-amd64",
		AllowInsecureLocal: true,
	}

	status := client.Check(context.Background())
	if status.Error != "" {
		t.Fatalf("unexpected check error: %s", status.Error)
	}
	if !status.Available {
		t.Fatalf("expected update to be available from live Releases API: %+v", status)
	}
	if status.LatestVersion != "1.0.1" {
		t.Fatalf("LatestVersion = %q, want 1.0.1", status.LatestVersion)
	}
}
