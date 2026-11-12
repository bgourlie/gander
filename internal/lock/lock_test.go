package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockExcludesAndTimesOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.0.lock")
	first, err := Acquire(context.Background(), path, Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}

	waited := false
	_, err = Acquire(context.Background(), path, Options{
		Timeout: 300 * time.Millisecond,
		Waiting: func() { waited = true },
	})
	var te *TimeoutError
	if !errors.As(err, &te) {
		t.Fatalf("second Acquire: %v, want a TimeoutError", err)
	}
	if !waited {
		t.Error("Waiting wasn't called")
	}

	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the lock file should stay: %v", err)
	}
	again, err := Acquire(context.Background(), path, Options{Timeout: time.Second})
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	again.Release()
}

func TestLockWaitsForRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.0.lock")
	first, _ := Acquire(context.Background(), path, Options{Timeout: time.Second})
	go func() {
		time.Sleep(300 * time.Millisecond)
		first.Release()
	}()
	start := time.Now()
	second, err := Acquire(context.Background(), path, Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	if time.Since(start) < 250*time.Millisecond {
		t.Error("the second Acquire didn't wait")
	}
}

func TestLockHonorsCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.0.lock")
	first, _ := Acquire(context.Background(), path, Options{Timeout: time.Second})
	defer first.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, path, Options{Timeout: time.Minute}); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Acquire = %v, want the context's error", err)
	}
}
