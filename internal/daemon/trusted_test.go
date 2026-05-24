package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheckTrustedPrefix_Valid — known-good path like /nix/store/abc123/bin/openvpn
// with prefix /nix/store/ → nil
func TestCheckTrustedPrefix_Valid(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nix", "store", "abc123", "bin", "openvpn")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0644); err != nil {
		t.Fatal(err)
	}

	prefix := filepath.Join(dir, "nix", "store") + "/"
	err := CheckTrustedPrefix(target, []string{prefix})
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// TestCheckTrustedPrefix_Invalid — /home/user/bin/evil.sh with prefix /nix/store/ → error
func TestCheckTrustedPrefix_Invalid(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "home", "user", "bin", "evil.sh")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0644); err != nil {
		t.Fatal(err)
	}

	prefix := filepath.Join(dir, "nix", "store") + "/"
	err := CheckTrustedPrefix(target, []string{prefix})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestCheckTrustedPrefix_EmptyPrefixes — /any/path with empty list → nil (validation disabled)
func TestCheckTrustedPrefix_EmptyPrefixes(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "any", "path")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0644); err != nil {
		t.Fatal(err)
	}

	err := CheckTrustedPrefix(target, []string{})
	if err != nil {
		t.Errorf("expected nil for empty prefixes, got %v", err)
	}
}

// TestCheckTrustedPrefix_ExactBinDir — /usr/bin/openvpn with prefix /usr/bin/ → nil
func TestCheckTrustedPrefix_ExactBinDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "usr", "bin", "openvpn")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0644); err != nil {
		t.Fatal(err)
	}

	prefix := filepath.Join(dir, "usr", "bin") + "/"
	err := CheckTrustedPrefix(target, []string{prefix})
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// TestCheckTrustedPrefix_NixosStyle — /nix/store/abc123-def456/bin/openvpn
// with prefix /nix/store/ → nil
func TestCheckTrustedPrefix_NixosStyle(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nix", "store", "abc123-def456", "bin", "openvpn")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0644); err != nil {
		t.Fatal(err)
	}

	prefix := filepath.Join(dir, "nix", "store") + "/"
	err := CheckTrustedPrefix(target, []string{prefix})
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// TestCheckTrustedPrefix_SymlinkResolution — create a temp symlink, resolve,
// check the resolved path against prefix
func TestCheckTrustedPrefix_SymlinkResolution(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real-target")
	if err := os.WriteFile(target, []byte("dummy"), 0644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "mylink")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	prefix := dir + "/"
	err := CheckTrustedPrefix(link, []string{prefix})
	if err != nil {
		t.Errorf("expected nil for symlink resolving under prefix, got %v", err)
	}
}

// TestCheckTrustedPrefix_NoPartialMatch — /nix/storage/bin/tool
// with prefix /nix/store/ → error
func TestCheckTrustedPrefix_NoPartialMatch(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nix", "storage", "bin", "tool")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0644); err != nil {
		t.Fatal(err)
	}

	prefix := filepath.Join(dir, "nix", "store") + "/"
	err := CheckTrustedPrefix(target, []string{prefix})
	if err == nil {
		t.Fatal("expected error for non-matching prefix, got nil")
	}
}
