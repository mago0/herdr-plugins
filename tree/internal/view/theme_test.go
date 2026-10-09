package view

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mago0/herdr-plugins/tree/internal/model"
)

func TestLoadTheme(t *testing.T) {
	def := DefaultTheme()
	if got := LoadTheme(filepath.Join(t.TempDir(), "absent")); got.TabGlyph != def.TabGlyph || got.Colors[model.Blocked] != def.Colors[model.Blocked] {
		t.Fatalf("no config must give the default theme, got %+v", got)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(`
[glyphs]
tab = "⇥"
[colors]
blocked = "#ff0000"
`), 0o644)
	got := LoadTheme(dir)
	if got.TabGlyph != "⇥" || got.PaneGlyph != def.PaneGlyph {
		t.Errorf("glyphs = %q %q", got.TabGlyph, got.PaneGlyph)
	}
	if got.Colors[model.Blocked] != "#ff0000" || got.Colors[model.Done] != def.Colors[model.Done] {
		t.Errorf("colors = %v", got.Colors)
	}
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte(`not = [toml`), 0o644)
	if got := LoadTheme(dir); got.TabGlyph != def.TabGlyph {
		t.Errorf("a config that does not parse must give the default theme, got %+v", got)
	}
}
