package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func loadTestConfig(t *testing.T) *Config {
	t.Helper()
	root := t.TempDir()
	t.Setenv("TEMP_DIR", filepath.Join(root, "tasks"))
	t.Setenv("OUTPUT_DIR", filepath.Join(root, "output"))
	return Load()
}

func TestLoadTightensPrivateStorageDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	root := t.TempDir()
	tempDir := filepath.Join(root, "tasks")
	outputDir := filepath.Join(root, "output")
	for _, path := range []string{tempDir, outputDir} {
		if err := os.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TEMP_DIR", tempDir)
	t.Setenv("OUTPUT_DIR", outputDir)

	_ = Load()
	for _, path := range []string{tempDir, outputDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0700 {
			t.Fatalf("storage directory %s mode = %o, want 700", path, got)
		}
	}
}

func TestLoadFailsClosedWhenPrivateStorageCannotBeCreated(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEMP_DIR", filepath.Join(blocked, "tasks"))
	t.Setenv("OUTPUT_DIR", filepath.Join(root, "output"))
	if err := Load().Validate(); err == nil {
		t.Fatal("storage initialization error must fail configuration validation")
	}
}

func TestLoadRejectsDangerouslyBroadStoragePathsBeforeChangingPermissions(t *testing.T) {
	t.Setenv("TEMP_DIR", string(filepath.Separator))
	t.Setenv("OUTPUT_DIR", filepath.Join(t.TempDir(), "output"))
	if err := Load().Validate(); err == nil {
		t.Fatal("filesystem root must be rejected")
	}

	t.Setenv("TEMP_DIR", os.TempDir())
	if err := Load().Validate(); err == nil {
		t.Fatal("shared temporary root must be rejected")
	}

	if home, err := os.UserHomeDir(); err == nil {
		t.Setenv("TEMP_DIR", home)
		if err := Load().Validate(); err == nil {
			t.Fatal("user home directory must be rejected")
		}
	}

	t.Setenv("TEMP_DIR", "relative/tasks")
	if err := Load().Validate(); err == nil {
		t.Fatal("relative storage directory must be rejected")
	}
}

func TestValidateRejectsStorageAliasesForTheSameDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic-link behavior differs on Windows")
	}
	root := t.TempDir()
	realDir := filepath.Join(root, "private")
	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatal(err)
	}
	aliasDir := filepath.Join(root, "private-alias")
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv("TEMP_DIR", realDir)
	t.Setenv("OUTPUT_DIR", aliasDir)
	if err := Load().Validate(); err == nil {
		t.Fatal("storage paths resolving to the same directory must be rejected")
	}
}

func TestValidateRejectsUnknownDrivers(t *testing.T) {
	cfg := loadTestConfig(t)
	cfg.TaskRepoDriver = "typo"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected unsupported repository driver to fail validation")
	}

	cfg = loadTestConfig(t)
	cfg.QueueDriver = "typo"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected unsupported queue driver to fail validation")
	}
}

func TestValidateRejectsExternalStateServices(t *testing.T) {
	cfg := loadTestConfig(t)
	cfg.TaskRepoDriver = "postgres"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Postgres task storage must not be accepted")
	}

	cfg = loadTestConfig(t)
	cfg.QueueDriver = "redis"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Redis queues must not be accepted")
	}
}

func TestValidateRejectsMalformedEnvironmentValues(t *testing.T) {
	t.Setenv("BEAUTIFY_ENABLED", "sometimes")
	cfg := loadTestConfig(t)
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected malformed boolean environment value to fail validation")
	}

	t.Setenv("BEAUTIFY_ENABLED", "true")
	t.Setenv("MAX_CONCURRENT_TASKS", "many")
	cfg = loadTestConfig(t)
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected malformed integer environment value to fail validation")
	}
}

func TestSafeFormattingDefaults(t *testing.T) {
	for _, key := range []string{"BEAUTIFY_ENABLED", "DEOBFUSCATE_ENABLED", "BEAUTIFY_TIMEOUT", "BEAUTIFY_MAX_FILE_SIZE"} {
		t.Setenv(key, "")
	}
	cfg := loadTestConfig(t)
	if !cfg.BeautifyEnabled {
		t.Fatal("safe formatting should be enabled by default")
	}
	if cfg.DeobfuscateEnabled {
		t.Fatal("heuristic deobfuscation must remain opt-in")
	}
	if cfg.BeautifyMaxFileSize < 8*1024*1024 {
		t.Fatalf("expected large runtime bundles to be supported, got %d", cfg.BeautifyMaxFileSize)
	}
}

func TestCORSOriginsAreOptInAndValidated(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	cfg := loadTestConfig(t)
	if len(cfg.CORSAllowedOrigins) != 0 {
		t.Fatalf("cross-origin access must be disabled by default: %v", cfg.CORSAllowedOrigins)
	}

	t.Setenv("CORS_ALLOWED_ORIGINS", "https://one.example, http://localhost:5173, https://one.example")
	cfg = loadTestConfig(t)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid origins rejected: %v", err)
	}
	if len(cfg.CORSAllowedOrigins) != 2 {
		t.Fatalf("expected trimmed, deduplicated origins, got %v", cfg.CORSAllowedOrigins)
	}

	for _, value := range []string{"https://example.test/path", "javascript:alert(1)", "*,https://example.test"} {
		t.Setenv("CORS_ALLOWED_ORIGINS", value)
		if err := loadTestConfig(t).Validate(); err == nil {
			t.Fatalf("expected invalid CORS configuration %q to fail", value)
		}
	}
}
