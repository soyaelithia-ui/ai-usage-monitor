package web

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedDistFS(t *testing.T) {
	fsys := DistFS()
	for _, required := range []string{"index.html", "favicon.svg", "theme-init.js", "fonts/inter-latin.woff2", "icons/codex.svg"} {
		if _, err := fs.Stat(fsys, required); err != nil {
			t.Fatalf("missing embedded static asset %s: %v", required, err)
		}
	}

	assets, err := fs.ReadDir(fsys, "assets")
	if err != nil || len(assets) == 0 {
		t.Fatalf("expected built assets in embedded dist/assets: %v", err)
	}

	var foundCSS bool
	for _, a := range assets {
		if strings.HasSuffix(a.Name(), ".css") {
			foundCSS = true
			raw, err := fs.ReadFile(fsys, "assets/"+a.Name())
			if err != nil {
				t.Fatalf("read css %s: %v", a.Name(), err)
			}
			css := string(raw)
			for _, token := range []string{
				"tabular-nums",
				"font-kerning:none",
				"cubic-bezier(.2,0,0,1)",
				"prefers-reduced-motion:reduce",
				"cute-pastel-blue",
				"burned-paper",
				"vivid-yellow-green",
			} {
				if !strings.Contains(strings.ReplaceAll(css, " ", ""), strings.ReplaceAll(token, " ", "")) {
					t.Fatalf("built CSS missing invariant token %q", token)
				}
			}
		}
	}
	if !foundCSS {
		t.Fatal("expected at least one compiled CSS bundle in embedded dist/assets")
	}
}
