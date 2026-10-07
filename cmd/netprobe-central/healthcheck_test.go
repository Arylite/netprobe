package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/Arylite/netprobe/internal/central/store/storetest"
)

func listen(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

func TestHealthcheckNeedsBothSurfacesListening(t *testing.T) {
	edge, api := listen(t), listen(t)
	if out, err := runCLI(t, "healthcheck", "--edge-listen", edge, "--api-listen", api); err != nil || out != "ok\n" {
		t.Fatalf("both listening: %q, %v", out, err)
	}

	// A closed port is a failure, and so is an address that makes no sense.
	for _, args := range [][]string{
		{"--edge-listen", edge, "--api-listen", "127.0.0.1:1"},
		{"--edge-listen", "127.0.0.1:1", "--api-listen", api},
		{"--edge-listen", "nonsense", "--api-listen", api},
	} {
		if _, err := runCLI(t, append([]string{"healthcheck"}, args...)...); err == nil {
			t.Errorf("%v: accepted", args)
		}
	}
}

func TestHealthcheckReachesAWildcardAddressThroughLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	if _, err := runCLI(t, "healthcheck", "--edge-listen", "0.0.0.0:"+port, "--api-listen", ":"+port); err != nil {
		t.Fatal(err)
	}
}

func TestTheDatabaseURLCanComeFromAFile(t *testing.T) {
	st, url := storetest.OpenURL(t)
	file := filepath.Join(t.TempDir(), "database_url")
	if err := os.WriteFile(file, []byte(url+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NETPROBE_DATABASE_URL", "")
	t.Setenv("NETPROBE_DATABASE_URL_FILE", file)

	if _, err := runCLI(t, "check", "add", "--id", "web", "--kind", "tcp", "--target", "example.com:443"); err != nil {
		t.Fatal(err)
	}
	if checks, _ := st.ListChecks(context.Background()); len(checks) != 1 {
		t.Fatalf("the check went elsewhere: %+v", checks)
	}
	// What is given on the command line wins over the file.
	if _, err := runCLI(t, "check", "list", "--database-url", url); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NETPROBE_DATABASE_URL_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := runCLI(t, "check", "list"); err == nil {
		t.Fatal("a missing file was ignored")
	}
}
