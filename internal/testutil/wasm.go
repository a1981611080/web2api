package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func ProjectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func BuildWASM(t *testing.T, srcDir string, outputPath string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", outputPath, srcDir)
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	cmd.Dir = ProjectRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build wasm %s: %v\n%s", srcDir, err, string(out))
	}
}

func BuildExampleEchoPlugin(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "echo.wasm")
	BuildWASM(t, "./examples/plugins/echo", path)
	return path
}

func BuildExampleHTTPContinuePlugin(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "http_continue.wasm")
	BuildWASM(t, "./examples/plugins/httpcontinue", path)
	return path
}
