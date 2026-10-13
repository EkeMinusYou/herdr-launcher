//go:build windows

package main

// Process and socket handling on Windows, where herdr's socket is a named pipe.

import (
	"errors"
	"net"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/Microsoft/go-winio"
)

const (
	detachedProcess        = 0x00000008
	createNewProcessGroup  = 0x00000200
	createBreakawayFromJob = 0x01000000
)

func dialHerdr(path string) (net.Conn, error) { return winio.DialPipe(path, nil) }

// Starts a process without a console and outside the popup's job, so it keeps running after the popup
// closes. A job that does not allow breaking away refuses that flag, so try once more without it.
func startDetached(name string, args ...string) error {
	flags := uint32(detachedProcess | createNewProcessGroup)
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags | createBreakawayFromJob}
	if err := cmd.Start(); err == nil {
		return nil
	}
	cmd = exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
	return cmd.Start()
}

// A command line run by cmd.exe, like herdr's own custom commands. The line is passed through as is,
// because cmd.exe does not parse arguments the way Go quotes them.
func shellCommand(command string) *exec.Cmd {
	cmd := exec.Command("cmd.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: "cmd.exe /d /c " + command}
	return cmd
}

// stopCommand stops the whole process tree, so nothing extra is needed.
func ownProcessGroup(*exec.Cmd) {}

func stopCommand(cmd *exec.Cmd) {
	exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}

// Windows cannot replace a running executable, so the launcher does not rebuild itself there.
func reexec(string) error { return errors.New("re-exec is not supported on Windows") }
