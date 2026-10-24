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
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureClient(t *testing.T, handler http.HandlerFunc) client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	return client{arch: "amd64", cache: t.TempDir(), http: &http.Client{CheckRedirect: trustedRedirect, Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		// Keep real validated URLs in the client; route only this test's transport
		// to a loopback fixture. No request can reach GitHub from these tests.
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Scheme, u.Host = target.Scheme, target.Host
		copy.URL = &u
		return transport.RoundTrip(copy)
	})}}
}

func testRelease(payload []byte) *Release {
	name, _ := installerName("amd64")
	base := "https://github.com/baseonmars/track-tagger/releases/download/v1.2.3/"
	return &Release{Version: "v1.2.3", PageURL: ReleasesURL, tag: "v1.2.3", installer: asset{Name: name, URL: base + name, Size: int64(len(payload))}, checksums: asset{Name: "SHA256SUMS", URL: base + "SHA256SUMS", Size: 100}}
}

func releaseJSON(r *Release) string {
	b, _ := json.Marshal(map[string]any{"tag_name": r.tag, "draft": false, "prerelease": false, "assets": []asset{r.installer, r.checksums}})
	return string(b)
}

func sumFor(payload []byte, name string) string {
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:]) + "  ./" + name + "\n"
}

func TestVersion(t *testing.T) {
	for _, s := range []string{"dev", "v1", "1.2", "1.2.3.4", "1.02.3", "v1.2.3-rc.1", "1.2.3+build", " 1.2.3", "V1.2.3", "-1.2.3", "18446744073709551616.0.0"} {
		if _, err := parseVersion(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
	a, err := parseVersion("v1.10.0")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := parseVersion("1.9.99")
	if !newer(a, b) || newer(b, a) || newer(a, a) {
		t.Fatal("incorrect numeric comparison")
	}
}

func TestCheckStableRelease(t *testing.T) {
	r := testRelease([]byte("installer"))
	c := fixtureClient(t, func(w http.ResponseWriter, rq *http.Request) {
		if rq.URL.Path != "/repos/baseonmars/track-tagger/releases/latest" {
			t.Errorf("unexpected path %s", rq.URL.Path)
		}
		io.WriteString(w, releaseJSON(r))
	})
	got, err := c.check(context.Background(), "1.2.2")
	if err != nil || got == nil || got.Version != "v1.2.3" || got.PageURL != "https://github.com/baseonmars/track-tagger/releases/tag/v1.2.3" {
		t.Fatalf("check: %+v %v", got, err)
	}
	for _, current := range []string{"1.2.3", "2.0.0"} {
		got, err := c.check(context.Background(), current)
		if err != nil || got != nil {
			t.Fatalf("offered downgrade/current: %+v %v", got, err)
		}
	}
	if _, err := c.check(context.Background(), "0.1.0-dev"); !errors.Is(err, ErrDevelopmentBuild) {
		t.Fatalf("development build: %v", err)
	}
	c.arch = "arm64"
	if _, err := c.check(context.Background(), "1.0.0"); err == nil {
		t.Fatal("accepted wrong architecture")
	}
}

func TestRejectMalformedRelease(t *testing.T) {
	r := testRelease([]byte("installer"))
	valid := releaseJSON(r)
	for _, body := range []string{
		"{}", "not json", valid + "{}", strings.Repeat(" ", maxMetadata+1),
		strings.Replace(valid, "v1.2.3", "v1.2.3-rc.1", 1),
		strings.Replace(valid, "https://github.com/", "http://github.com/", 1),
		strings.Replace(valid, "baseonmars/track-tagger/releases/download", "attacker/track-tagger/releases/download", 1),
		strings.Replace(valid, `"draft":false,`, "", 1),
	} {
		c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, body) })
		if _, err := c.check(context.Background(), "1.0.0"); err == nil {
			t.Errorf("accepted malformed metadata %.100s", body)
		}
	}
	for _, field := range []string{"draft", "prerelease"} {
		body := strings.Replace(valid, `"`+field+`":false`, `"`+field+`":true`, 1)
		c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, body) })
		if got, err := c.check(context.Background(), "1.0.0"); err != nil || got != nil {
			t.Errorf("accepted %s release", field)
		}
	}
}

func TestDuplicateAndOversizedAssets(t *testing.T) {
	r := testRelease([]byte("installer"))
	for _, assets := range [][]asset{
		{r.installer, r.installer, r.checksums},
		{r.installer, r.checksums, r.checksums},
		{{Name: r.installer.Name, URL: r.installer.URL, Size: maxInstaller + 1}, r.checksums},
		{r.installer},
	} {
		b, _ := json.Marshal(map[string]any{"tag_name": r.tag, "draft": false, "prerelease": false, "assets": assets})
		c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) { w.Write(b) })
		if _, err := c.check(context.Background(), "1.0.0"); err == nil {
			t.Fatal("accepted invalid asset set")
		}
	}
}

func TestChecksum(t *testing.T) {
	name := "track-tagger-windows-x64-setup.exe"
	good := sumFor([]byte("installer"), name)
	if _, err := checksum([]byte(good), name); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", good + good, strings.Replace(good, "  ./", "x  ./", 1), strings.Replace(good, name, "track-tagger-windows-arm64-setup.exe", 1), "garbage\n" + good, strings.Replace(good, "  ./", "  ../", 1)} {
		if _, err := checksum([]byte(bad), name); err == nil {
			t.Errorf("accepted checksum %q", bad)
		}
	}
}

func TestDownloadVerifiedAndCleanup(t *testing.T) {
	payload := []byte("synthetic executable bytes; never executed")
	r := testRelease(payload)
	c := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "SHA256SUMS") {
			io.WriteString(w, sumFor(payload, r.installer.Name))
			return
		}
		w.Write(payload)
	})
	var last int64
	s, err := c.download(context.Background(), r, func(n, total int64) {
		if n < last || total != int64(len(payload)) {
			t.Error("invalid progress")
		}
		last = n
	})
	if err != nil {
		t.Fatal(err)
	}
	path := s.dir
	b, err := os.ReadFile(filepath.Join(path, "installer.exe"))
	if err != nil || string(b) != string(payload) || last != int64(len(payload)) {
		t.Fatalf("download mismatch: %v", err)
	}
	s.Cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cleanup: %v", err)
	}
}

func TestDownloadFailureRemovesPartialFiles(t *testing.T) {
	payload := []byte(strings.Repeat("x", 128<<10))
	r := testRelease(payload)
	for _, scenario := range []string{"checksum", "oversize", "truncated", "http", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := fixtureClient(t, func(w http.ResponseWriter, req *http.Request) {
				if strings.HasSuffix(req.URL.Path, "SHA256SUMS") {
					p := payload
					if scenario == "checksum" {
						p = []byte("wrong")
					}
					io.WriteString(w, sumFor(p, r.installer.Name))
					return
				}
				switch scenario {
				case "oversize":
					w.Write(append(payload, 'x'))
				case "truncated":
					w.Write(payload[:100])
				case "http":
					w.WriteHeader(http.StatusForbidden)
				default:
					w.Write(payload)
				}
			})
			_, err := c.download(ctx, r, func(int64, int64) {
				if scenario == "cancel" {
					cancel()
				}
			})
			if err == nil {
				t.Fatal("expected failure")
			}
			entries, _ := os.ReadDir(c.cache)
			if len(entries) != 0 {
				t.Fatalf("partial staging directory retained: %v", entries)
			}
		})
	}
}

func TestTrustedRedirects(t *testing.T) {
	for _, raw := range []string{"http://github.com/baseonmars/track-tagger/releases/download/v1.0.0/x", "https://github.com.evil.test/baseonmars/track-tagger", "https://github.com/other/repo/releases/download/v1/x", "https://user@release-assets.githubusercontent.com/x", "https://release-assets.githubusercontent.com:444/x", "https://example.com/setup.exe", "file:///tmp/setup.exe"} {
		u, _ := url.Parse(raw)
		if trustedRedirect(&http.Request{URL: u}, nil) == nil {
			t.Errorf("trusted %s", raw)
		}
	}
	for _, raw := range []string{latestURL, "https://release-assets.githubusercontent.com/github-production-release-asset/x?sig=abc", "https://objects.githubusercontent.com/github-production-release-asset/x"} {
		u, _ := url.Parse(raw)
		if err := trustedRedirect(&http.Request{URL: u}, nil); err != nil {
			t.Error(err)
		}
		if err := trustedRedirect(&http.Request{URL: u}, make([]*http.Request, 5)); err == nil {
			t.Fatal("unbounded redirects")
		}
	}
}

func TestCanceledAndHTTPCheck(t *testing.T) {
	c := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
	if _, err := c.check(context.Background(), "1.0.0"); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("rate limit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.check(ctx, "1.0.0"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestHelperArgumentsAndStaging(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "update-test")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "handoff.json")
	h, path, err := helperArguments([]string{helperFlag, "123", manifest})
	if err != nil || h != 123 || path != manifest {
		t.Fatalf("args: %v", err)
	}
	for _, args := range [][]string{{helperFlag}, {helperFlag, "0", manifest}, {helperFlag, "-1", manifest}, {helperFlag, "123", "handoff.json"}, {helperFlag, "123", filepath.Join(dir, "other.json")}, {helperFlag, "123", manifest, "extra"}} {
		if _, _, err := helperArguments(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if err := safeStage(dir, root); err != nil {
		t.Fatal(err)
	}
	if err := safeStage(root, root); err == nil {
		t.Fatal("accepted root directory")
	}
	if err := safeStage(dir, t.TempDir()); err == nil {
		t.Fatal("accepted unrelated root")
	}
	for _, s := range []string{"", strings.Repeat("z", 64), strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		if validHash(s) {
			t.Fatalf("accepted hash %q", s)
		}
	}
}

func TestHandoffWaitInstallRestartOrdering(t *testing.T) {
	for _, failure := range []string{"", "wait", "install", "restart"} {
		var events []string
		step := func(name string) func() error {
			return func() error {
				events = append(events, name)
				if failure == name {
					return fmt.Errorf("%s failed", name)
				}
				return nil
			}
		}
		err := finishHandoff(step("wait"), step("install"), step("restart"))
		want := "wait,install,restart"
		if failure == "wait" {
			want = "wait"
		}
		if strings.Join(events, ",") != want {
			t.Fatalf("order: %v", events)
		}
		if (err != nil) != (failure != "") {
			t.Fatalf("failure %s: %v", failure, err)
		}
	}
}

func TestCleanupRejectsUnownedOrChangedStages(t *testing.T) {
	for _, kind := range []string{"download", "unknown-file", "changed-helper", "nested-file", "invalid-marker"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "update-test")
			if e := os.Mkdir(dir, 0700); e != nil {
				t.Fatal(e)
			}
			helper := filepath.Join(dir, "update-helper.exe")
			if e := os.WriteFile(helper, []byte("synthetic helper"), 0600); e != nil {
				t.Fatal(e)
			}
			hash, e := fileHash(helper, maxInstaller)
			if e != nil {
				t.Fatal(e)
			}
			h := handoff{ParentPID: 1, ReadyEvent: 1, InstallDir: t.TempDir(), HelperHash: hash, InstallerHash: hash}
			b, _ := json.Marshal(h)
			if kind != "download" {
				if e = os.WriteFile(filepath.Join(dir, "cleanup.json"), b, 0600); e != nil {
					t.Fatal(e)
				}
			}
			switch kind {
			case "unknown-file":
				e = os.WriteFile(filepath.Join(dir, "unrelated.txt"), []byte("keep"), 0600)
			case "changed-helper":
				e = os.WriteFile(helper, []byte("external edit"), 0600)
			case "nested-file":
				e = os.Mkdir(filepath.Join(dir, "nested"), 0700)
			case "invalid-marker":
				e = os.WriteFile(filepath.Join(dir, "cleanup.json"), []byte("not JSON"), 0600)
			}
			if e != nil {
				t.Fatal(e)
			}
			locked := false
			e = cleanupStages(root, func(string) (io.Closer, error) { locked = true; return io.NopCloser(strings.NewReader("")), nil })
			if e != nil {
				t.Fatal(e)
			}
			if locked {
				t.Fatal("unsafe stage reached deletion guard")
			}
			if _, e = os.Stat(helper); e != nil {
				t.Fatal("unowned/changed stage removed")
			}
		})
	}
}
