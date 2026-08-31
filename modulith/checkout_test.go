package modulith

import (
	"bytes"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Exercise Git's real checkout filter without changing the index or worktree.
func TestGoCheckoutLineEndings(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", ".git")); os.IsNotExist(err) {
		t.Skip("source archive: no Git checkout to check")
	}
	cmd := exec.Command("git", "-c", "core.autocrlf=true", "cat-file", "--filters", "HEAD:modulith/load.go")
	cmd.Dir = ".."
	data, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("\r\n")) {
		t.Fatal("Go checkout becomes CRLF with core.autocrlf=true; gofmt will reject it")
	}
	formatted, err := format.Source(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, formatted) {
		t.Fatal("checkout filter changed gofmt-formatted Go source")
	}
}
