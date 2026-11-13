package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Killing the proxy, as an editor stops its language server, ends goose too; and goose's
// exit code comes back whole, even one that doesn't fit in a byte.
func TestProxyOnWindows(t *testing.T) {
	e := newE2E(t)
	if _, _, code := e.run(e.proj, "goose", []string{"GOOSE_TEST_EXIT=3221225786"}, "+v1.0", "build"); code != 0xC000013A {
		t.Errorf("exit code %#x, want 0xC000013A", code)
	}

	pidFile := filepath.Join(t.TempDir(), "pid")
	proxy := e.command(e.proj, "goose", nil, "+v1.0", "sleep", pidFile)
	if err := proxy.Start(); err != nil {
		t.Fatal(err)
	}
	defer proxy.Wait()
	var pid int
	for start := time.Now(); pid == 0; time.Sleep(50 * time.Millisecond) {
		if time.Since(start) > 30*time.Second {
			proxy.Process.Kill()
			t.Fatal("goose never started")
		}
		b, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(string(b))
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(child)
	proxy.Process.Kill()
	if ev, _ := windows.WaitForSingleObject(child, 10_000); ev != windows.WAIT_OBJECT_0 {
		windows.TerminateProcess(child, 1)
		t.Error("goose outlived the proxy")
	}
}
