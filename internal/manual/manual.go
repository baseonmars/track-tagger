// Package manual renders the illustrated Track Tagger user guide as one offline HTML document.
package manual

import (
	"bytes"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"image/png"
	"io/fs"
	"sync"
)

//go:embed manual.html images
var assets embed.FS

var screenshots = []string{
	"launcher", "library", "review", "mapping", "playback", "history", "updates",
}

var rendered [2]struct {
	once sync.Once
	page []byte
	err  error
}

type pageData struct {
	Web    bool
	Images map[string]template.URL
}

// Render returns a complete HTML document with its CSS and PNG screenshots embedded.
// When web is true, the header links to the live Recordings, History, and Help pages.
// The standalone form keeps only local document navigation.
func Render(web bool) ([]byte, error) {
	index := 0
	if web {
		index = 1
	}
	entry := &rendered[index]
	entry.once.Do(func() { entry.page, entry.err = render(web) })
	return bytes.Clone(entry.page), entry.err
}

func render(web bool) ([]byte, error) {
	data := pageData{Web: web, Images: make(map[string]template.URL, len(screenshots))}
	for _, name := range screenshots {
		path := "images/" + name + ".png"
		b, err := fs.ReadFile(assets, path)
		if err != nil {
			return nil, fmt.Errorf("manual screenshot %s: %w", path, err)
		}
		if _, err := png.Decode(bytes.NewReader(b)); err != nil {
			return nil, fmt.Errorf("manual screenshot %s: %w", path, err)
		}
		data.Images[name] = template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(b))
	}
	t, err := template.New("manual").ParseFS(assets, "manual.html")
	if err != nil {
		return nil, fmt.Errorf("parse manual: %w", err)
	}
	var out bytes.Buffer
	if err := t.ExecuteTemplate(&out, "manual.html", data); err != nil {
		return nil, fmt.Errorf("render manual: %w", err)
	}
	return out.Bytes(), nil
}
