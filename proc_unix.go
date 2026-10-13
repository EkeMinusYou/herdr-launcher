//go:build !windows

package main

// Process and socket handling on macOS and Linux.

import (
	"net"
	"os"
	"os/exec"
	"syscall"
)

func dialHerdr(path string) (net.Conn, error) { return net.Dial("unix", path) }

// Starts a process in a new session, so it keeps running after the popup that started it closes.
func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// A command line run by the shell.
func shellCommand(command string) *exec.Cmd { return exec.Command("sh", "-c", command) }

// Puts cmd in its own process group so stopCommand also stops what it started.
// Only for commands that do not use the terminal: a background process group cannot read it.
func ownProcessGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

func stopCommand(cmd *exec.Cmd) { syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }

// Replaces this process with self, keeping the arguments and environment.
func reexec(self string) error { return syscall.Exec(self, os.Args, os.Environ()) }
