package classifier

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectGamePackageByGameJS(t *testing.T) {
	// Unity mini-games ship game.js + app-config.json (config is named like a
	// mini-program) and must still be classified as a game package.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "game.js"), []byte("GameGlobal"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-config.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	data := []byte{0xBE, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xED}
	p, err := DetectPackageProfile(data, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsGamePackage {
		t.Fatalf("game.js must mark package as game, got variant=%s", p.SuspectedVariant)
	}
	if p.SuspectedVariant != "game" {
		t.Fatalf("variant = %s, want game", p.SuspectedVariant)
	}
}

func TestDetectMiniProgramNotGame(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app-service.js"), []byte("App({})"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app-config.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	data := []byte{0xBE, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xED}
	p, err := DetectPackageProfile(data, dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.IsGamePackage {
		t.Fatalf("mini-program must not be flagged as game")
	}
}
