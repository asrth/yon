// Package updater checks GitHub Releases for a newer Yon build and downloads the
// release asset matching the host OS. It performs no install / self-replace: it
// fetches the asset to a folder and leaves the user to install it. The package
// is UI-free so it can be unit-tested without Fyne.
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// apiURL is the GitHub "latest release" endpoint. It is a var so tests can point
// it at a local server.
var apiURL = "https://api.github.com/repos/asrth/yon/releases/latest"

// httpClient has no global timeout; callers pass a context deadline sized to the
// operation (a quick check vs. a large download).
var httpClient = &http.Client{}

// Asset is one downloadable file attached to a release.
type Asset struct {
	Name string
	URL  string
	Size int64
}

// Release is the subset of a GitHub release we use.
type Release struct {
	Version string // tag without a leading "v", e.g. "0.4.0"
	TagName string // raw tag, e.g. "v0.4.0"
	HTMLURL string // release page
	Assets  []Asset
}

// Latest fetches the latest published release.
func Latest(ctx context.Context) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("User-Agent", "Yon-updater")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github: unexpected status %s", resp.Status)
	}

	var raw struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return Release{}, err
	}
	rel := Release{
		TagName: raw.TagName,
		HTMLURL: raw.HTMLURL,
		Version: strings.TrimPrefix(strings.TrimSpace(raw.TagName), "v"),
	}
	for _, a := range raw.Assets {
		rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size})
	}
	return rel, nil
}

// AssetFor returns the release asset that matches the given GOOS (runtime.GOOS).
func (r Release) AssetFor(goos string) (Asset, bool) {
	var key string
	switch goos {
	case "darwin":
		key = "macos"
	case "windows":
		key = "windows"
	case "linux":
		key = "linux"
	default:
		return Asset{}, false
	}
	for _, a := range r.Assets {
		if strings.Contains(strings.ToLower(a.Name), key) {
			return a, true
		}
	}
	return Asset{}, false
}

// IsNewer reports whether remote is a higher semantic version than current.
// Both may carry a leading "v"; pre-release / build suffixes are ignored.
func IsNewer(current, remote string) bool {
	c := parseVer(current)
	r := parseVer(remote)
	if c == nil || r == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if r[i] != c[i] {
			return r[i] > c[i]
		}
	}
	return false
}

// parseVer turns "v1.2.3", "1.2", or "1.2.3-rc1" into [3]int{major,minor,patch}.
// It returns nil if a numeric component cannot be parsed.
func parseVer(s string) []int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	out := make([]int, 3)
	for i := 0; i < 3; i++ {
		if i >= len(parts) {
			continue
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}

// Download writes the asset into dir and returns the saved file path.
func Download(ctx context.Context, a Asset, dir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Yon-updater")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: unexpected status %s", resp.Status)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	dest := filepath.Join(dir, a.Name)
	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return dest, nil
}

// DownloadsDir returns the user's Downloads folder, falling back to the home
// directory (then the OS temp dir) when it does not exist.
func DownloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	if d := filepath.Join(home, "Downloads"); isDir(d) {
		return d
	}
	return home
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// Reveal opens the host file manager with the downloaded file selected (or its
// folder opened on Linux).
func Reveal(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,"+path).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(path)).Start()
	}
}

// OpenFile opens path with the host's default handler — as if the user
// double-clicked it — e.g. mounting a .dmg or opening a .zip / .tar.xz. It waits
// for the launcher to hand off so the caller can safely quit afterwards.
func OpenFile(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Run()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", path).Run()
	default:
		return exec.Command("xdg-open", path).Run()
	}
}

// checkTimeout / downloadTimeout are sensible context deadlines for callers.
const (
	CheckTimeout    = 15 * time.Second
	DownloadTimeout = 10 * time.Minute
)
