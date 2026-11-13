package proxy

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/bgourlie/gander/internal/platform"
)

// run starts the compiler as a child, since Windows has no exec, and returns its exit
// code. The child gets the proxy's own stdin, stdout and stderr, and the environment
// unchanged. argv[0] is the absolute path; Goose finds its stdlib from the OS's record of
// its executable, and Go's quoting of the other arguments round-trips through the
// parsing of goose.exe's C runtime.
func run(path string, args []string) int {
	job := joinJob()

	// The console sends Ctrl-C to every process attached to it, so goose gets its own and
	// decides what it means. The proxy takes it and drops it, so that it lives to pass
	// on goose's exit code. signal.Ignore does nothing on Windows, and
	// SetConsoleCtrlHandler(NULL, TRUE) would be inherited by goose, turning Ctrl-C off.
	signal.Notify(make(chan os.Signal, 1), os.Interrupt, syscall.SIGTERM)

	cmd := exec.Command(path, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if !hasConsole() {
		// A console program started without a console gets a new, visible window.
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	}
	err := cmd.Run()
	if job != 0 {
		// Goose is done. Whatever it started, such as a program from "goose run", lives
		// on after the proxy exits, as it would have if goose ran alone (cargo#5598).
		setJobLimits(job, 0)
	}
	if cmd.ProcessState == nil {
		fmt.Fprintf(os.Stderr, "gander: can't run %s: %v\n", path, platform.ExplainExecError(path, err))
		return 1
	}
	// ExitCode keeps 32-bit codes such as 0xC000013A, which os.Exit passes on whole.
	return cmd.ProcessState.ExitCode()
}

// joinJob puts the proxy in a job that ends goose when the proxy is killed, as an editor
// stops its language server; otherwise goose lives on (uv#17492). It joins before goose
// starts, since adding goose afterwards races its exit (uv#18170). Without a job only
// that is lost, so a failure is silent. It returns 0 when there's no job.
func joinJob() windows.Handle {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	// BREAKAWAY_OK lets a program that asks to leave the job do so; SILENT_BREAKAWAY_OK
	// would take goose out of it too.
	if setJobLimits(job, windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE|windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK) != nil ||
		windows.AssignProcessToJobObject(job, windows.CurrentProcess()) != nil {
		windows.CloseHandle(job)
		return 0
	}
	return job
}

func setJobLimits(job windows.Handle, flags uint32) error {
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = flags
	_, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	return err
}

var getConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

// hasConsole reports whether the proxy has a console, hidden or not. GetConsoleWindow
// would miss a hidden one, which an editor that starts the proxy with CREATE_NO_WINDOW
// gives it, and which goose then shares.
func hasConsole() bool {
	var pids [1]uint32
	n, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), 1)
	return n != 0
}
