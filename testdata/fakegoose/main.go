// Command fakegoose stands in for Goose's compiler in main_test.go, on every OS. Run as
// "goose version" it prints "goose <tag> (runtime test)", taking the tag from its
// folder's name. "goose sleep <file>" writes its process ID to file and waits a minute.
// Anything else prints its argv[0] and arguments, one per line, and exits with
// GOOSE_TEST_EXIT.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func main() {
	args := os.Args[1:]
	switch {
	case len(args) == 1 && args[0] == "version":
		exe, _ := os.Executable()
		fmt.Printf("goose %s (runtime test)\n", filepath.Base(filepath.Dir(exe)))
		return
	case len(args) == 2 && args[0] == "sleep":
		// Renamed into place, so the test never reads part of the number.
		os.WriteFile(args[1]+".tmp", []byte(strconv.Itoa(os.Getpid())), 0o644)
		os.Rename(args[1]+".tmp", args[1])
		time.Sleep(time.Minute)
		return
	}
	fmt.Println("argv0=" + os.Args[0])
	for _, a := range args {
		fmt.Println("arg=" + a)
	}
	code, _ := strconv.Atoi(os.Getenv("GOOSE_TEST_EXIT"))
	os.Exit(code)
}
