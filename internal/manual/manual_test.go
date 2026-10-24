package manual

import (
	"bytes"
	"encoding/base64"
	"html"
	"image/png"
	"regexp"
	"strings"
	"testing"
)

func TestRenderIsCompleteAndOffline(t *testing.T) {
	for _, web := range []bool{false, true} {
		name := "standalone"
		if web {
			name = "web"
		}
		t.Run(name, func(t *testing.T) {
			page, err := Render(web)
			if err != nil {
				t.Fatal(err)
			}
			s := string(page)
			for _, anchor := range []string{"install", "modes", "library", "review", "mapping", "apply", "playback", "history", "updates", "recovery", "troubleshooting", "cli"} {
				if !strings.Contains(s, `id="`+anchor+`"`) || !strings.Contains(s, `href="#`+anchor+`"`) {
					t.Errorf("missing section or contents link %q", anchor)
				}
			}
			if strings.Contains(s, `<link rel="stylesheet"`) || strings.Contains(s, `<script`) || strings.Contains(s, `src="http`) {
				t.Error("manual depends on external assets")
			}
			images := regexp.MustCompile(`src="data:image/png;base64,([A-Za-z0-9+/=]+)"`).FindAllStringSubmatch(html.UnescapeString(s), -1)
			if len(images) != len(screenshots) {
				t.Fatalf("got %d PNG images, want %d", len(images), len(screenshots))
			}
			for i, match := range images {
				name := screenshots[i] + ".png"
				b, err := base64.StdEncoding.DecodeString(match[1])
				if err != nil {
					t.Fatalf("decode %s: %v", name, err)
				}
				img, err := png.Decode(bytes.NewReader(b))
				if err != nil {
					t.Errorf("invalid %s: %v", name, err)
				} else if img.Bounds().Dx() < 320 || img.Bounds().Dy() < 160 {
					t.Errorf("%s is too small to be an application screenshot: %v", name, img.Bounds())
				}
			}
			if web {
				for _, path := range []string{`href="/"`, `href="/history"`, `href="/help"`} {
					if !strings.Contains(s, path) {
						t.Errorf("missing web navigation %s", path)
					}
				}
			} else if strings.Contains(s, `href="/"`) || strings.Contains(s, `href="/history"`) || strings.Contains(s, `href="/help"`) {
				t.Error("standalone guide has web navigation")
			}
		})
	}
}

func TestWebAndStandaloneShareManualBody(t *testing.T) {
	standalone, err := Render(false)
	if err != nil {
		t.Fatal(err)
	}
	web, err := Render(true)
	if err != nil {
		t.Fatal(err)
	}
	marker := []byte(`<main id="main">`)
	a := bytes.Index(standalone, marker)
	b := bytes.Index(web, marker)
	if a < 0 || b < 0 || !bytes.Equal(standalone[a:], web[b:]) {
		t.Fatal("web and standalone manual content differs")
	}
}
