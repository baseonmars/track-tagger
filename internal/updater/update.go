// Package updater discovers and stages verified stable Windows releases.
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
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const ReleasesURL = "https://github.com/baseonmars/track-tagger/releases/latest"
const latestURL = "https://api.github.com/repos/baseonmars/track-tagger/releases/latest"
const maxMetadata = 1 << 20
const maxInstaller = 512 << 20

var ErrDevelopmentBuild = errors.New("development builds do not update automatically; download a stable release from GitHub")

type version [3]uint64

var stableVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func parseVersion(s string) (version, error) {
	var v version
	m := stableVersion.FindStringSubmatch(s)
	if m == nil {
		return v, errors.New("expected a stable semantic version such as v1.2.3")
	}
	for i := range v {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return v, err
		}
		v[i] = n
	}
	return v, nil
}

func newer(a, b version) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release contains validated metadata for the matching architecture only.
type Release struct {
	Version   string
	PageURL   string
	tag       string
	installer asset
	checksums asset
}

type client struct {
	http  *http.Client
	arch  string
	cache string
}

func newClient() client {
	return client{http: &http.Client{Timeout: 15 * time.Minute, CheckRedirect: trustedRedirect}, arch: runtime.GOARCH}
}

func trustedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return false
	}
	switch u.Host {
	case "api.github.com":
		return u.Path == "/repos/baseonmars/track-tagger/releases/latest" && u.RawQuery == ""
	case "github.com":
		return strings.HasPrefix(u.Path, "/baseonmars/track-tagger/releases/download/") && u.RawQuery == ""
	case "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}

func trustedRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 || !trustedURL(req.URL.String()) {
		return errors.New("update download redirected to an untrusted location")
	}
	return nil
}

func (c client) get(ctx context.Context, raw string) (*http.Response, error) {
	if !trustedURL(raw) {
		return nil, errors.New("untrusted update URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Track-Tagger-Updater")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub update request failed: HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func boundedRead(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err == nil && int64(len(b)) > max {
		err = errors.New("update response exceeds its size limit")
	}
	return b, err
}

func installerName(arch string) (string, error) {
	switch arch {
	case "amd64":
		return "track-tagger-windows-x64-setup.exe", nil
	case "arm64":
		return "track-tagger-windows-arm64-setup.exe", nil
	}
	return "", errors.New("updates support Windows x64 and ARM64 only")
}

func validAsset(a asset, tag, name string, max int64) bool {
	return a.Name == name && a.Size > 0 && a.Size <= max && a.URL == "https://github.com/baseonmars/track-tagger/releases/download/"+tag+"/"+name
}

// Check returns nil when no newer stable release is available.
func Check(ctx context.Context, currentVersion string) (*Release, error) {
	return newClient().check(ctx, currentVersion)
}

func (c client) check(ctx context.Context, currentVersion string) (*Release, error) {
	current, err := parseVersion(currentVersion)
	if err != nil {
		return nil, ErrDevelopmentBuild
	}
	name, err := installerName(c.arch)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := c.get(ctx, latestURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := boundedRead(resp.Body, maxMetadata)
	if err != nil {
		return nil, err
	}
	var data struct {
		Tag        string  `json:"tag_name"`
		Draft      *bool   `json:"draft"`
		Prerelease *bool   `json:"prerelease"`
		Assets     []asset `json:"assets"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, fmt.Errorf("invalid release metadata: %w", err)
	}
	if data.Draft == nil || data.Prerelease == nil {
		return nil, errors.New("release metadata is missing publication status")
	}
	if *data.Draft || *data.Prerelease {
		return nil, nil
	}
	v, err := parseVersion(data.Tag)
	if err != nil {
		return nil, fmt.Errorf("invalid stable release version: %w", err)
	}
	if !newer(v, current) {
		return nil, nil
	}
	r := &Release{Version: data.Tag, PageURL: "https://github.com/baseonmars/track-tagger/releases/tag/" + data.Tag, tag: data.Tag}
	for _, a := range data.Assets {
		switch a.Name {
		case name:
			if r.installer.Name != "" || !validAsset(a, data.Tag, name, maxInstaller) {
				return nil, errors.New("invalid or duplicate installer asset")
			}
			r.installer = a
		case "SHA256SUMS":
			if r.checksums.Name != "" || !validAsset(a, data.Tag, "SHA256SUMS", maxMetadata) {
				return nil, errors.New("invalid or duplicate checksum asset")
			}
			r.checksums = a
		}
	}
	if r.installer.Name == "" || r.checksums.Name == "" {
		return nil, errors.New("release is missing the matching installer or SHA256SUMS")
	}
	return r, nil
}

// Staged owns one private update directory. Cleanup removes only that directory.
type Staged struct {
	dir  string
	hash string
}

func (s *Staged) Cleanup() {
	if s != nil && s.dir != "" {
		_ = os.RemoveAll(s.dir)
	}
}

func checksum(b []byte, name string) (string, error) {
	var found string
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			if strings.TrimSpace(line) != "" {
				return "", errors.New("malformed SHA256SUMS")
			}
			continue
		}
		file := strings.TrimPrefix(fields[1], "*")
		file = strings.TrimPrefix(file, "./")
		if file != name {
			continue
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size || found != "" {
			return "", errors.New("invalid or duplicate installer checksum")
		}
		found = strings.ToLower(fields[0])
	}
	if found == "" {
		return "", errors.New("SHA256SUMS has no matching installer entry")
	}
	return found, nil
}

func updateRoot() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "track-tagger", "updates"), nil
}

func Download(ctx context.Context, release *Release, progress func(received, total int64)) (*Staged, error) {
	return newClient().download(ctx, release, progress)
}

func (c client) download(ctx context.Context, release *Release, progress func(int64, int64)) (_ *Staged, resultErr error) {
	if release == nil {
		return nil, errors.New("missing release")
	}
	name, err := installerName(c.arch)
	if err != nil || !validAsset(release.installer, release.tag, name, maxInstaller) || !validAsset(release.checksums, release.tag, "SHA256SUMS", maxMetadata) {
		return nil, errors.New("invalid release assets")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	resp, err := c.get(ctx, release.checksums.URL)
	if err != nil {
		return nil, err
	}
	sums, err := boundedRead(resp.Body, maxMetadata)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	want, err := checksum(sums, name)
	if err != nil {
		return nil, err
	}
	root := c.cache
	if root == "" {
		root, err = updateRoot()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(root, "update-")
	if err != nil {
		return nil, err
	}
	s := &Staged{dir: dir, hash: want}
	defer func() {
		if resultErr != nil {
			s.Cleanup()
		}
	}()
	f, err := os.OpenFile(filepath.Join(dir, "installer.exe"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	resp, err = c.get(ctx, release.installer.URL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.ContentLength > maxInstaller || (resp.ContentLength > 0 && resp.ContentLength != release.installer.Size) {
		return nil, errors.New("installer size does not match release metadata")
	}
	h := sha256.New()
	var received int64
	buf := make([]byte, 64<<10)
	reader := io.LimitReader(resp.Body, release.installer.Size+1)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, readErr := reader.Read(buf)
		if n > 0 {
			received += int64(n)
			if received > release.installer.Size {
				return nil, errors.New("installer exceeds declared size")
			}
			if _, err := f.Write(buf[:n]); err != nil {
				return nil, err
			}
			_, _ = h.Write(buf[:n])
			if progress != nil {
				progress(received, release.installer.Size)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	if received != release.installer.Size || hex.EncodeToString(h.Sum(nil)) != want {
		return nil, errors.New("installer SHA-256 or size verification failed")
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return s, nil
}
