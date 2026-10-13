package main

// Shell commands run inside the picker, which stays open and streams their output into the preview,
// so the result can be read. Commands marked interactive run in the terminal after the picker closes instead.

import (
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
)

const maxOutput = 64 << 10 // keep the last this many bytes of a command's output

// The output of the last run of a command.
type commandOutput struct {
	text    string
	running bool
	err     error // why it failed, after it finished

	closeOnSuccess bool
}

// A command running inside the picker.
type runningCommand struct {
	cmd    *exec.Cmd
	exited atomic.Bool // set once it has been waited for, after which its process id may belong to someone else
}

// Stops the command and what it started, unless it has already exited.
func (r *runningCommand) stop() {
	if !r.exited.Load() {
		stopCommand(r.cmd)
	}
}

type outputMsg struct{ id, chunk string }

type exitMsg struct {
	id  string
	err error
}

// Starts it.command with its output going to a channel, and returns the process and the command
// that delivers the first message. Each outputMsg is followed by another wait on the channel.
func startCommand(it item) (*runningCommand, <-chan tea.Msg, error) {
	cmd := shellCommand(it.command)
	ownProcessGroup(cmd)
	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return nil, nil, err
	}
	w.Close() // the child holds its own copy; reading ends when it exits
	run := &runningCommand{cmd: cmd}
	ch := make(chan tea.Msg)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				ch <- outputMsg{it.id, string(buf[:n])}
			}
			if err != nil {
				if err != io.EOF {
					ch <- outputMsg{it.id, err.Error()}
				}
				break
			}
		}
		r.Close()
		err := cmd.Wait()
		run.exited.Store(true)
		ch <- exitMsg{it.id, err}
		close(ch)
	}()
	return run, ch, nil
}

func waitOutput(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func (o *commandOutput) add(chunk string) {
	o.text += strings.ReplaceAll(chunk, "\r\n", "\n")
	if len(o.text) > maxOutput {
		o.text = o.text[len(o.text)-maxOutput:]
	}
}

// The output as lines, where a carriage return (as progress bars use) overwrites the line.
func (o *commandOutput) lines() []string {
	lines := strings.Split(strings.TrimRight(o.text, "\n"), "\n")
	for i, l := range lines {
		if j := strings.LastIndex(l, "\r"); j >= 0 {
			lines[i] = l[j+1:]
		}
	}
	return lines
}

// What the run is doing or how it ended, and its color: running, done, or how it failed.
func (o *commandOutput) status() (string, color.Color) {
	var exit *exec.ExitError
	switch {
	case o.running:
		return "running…", yellow
	case o.err == nil:
		return "done", green
	case errors.As(o.err, &exit):
		return fmt.Sprintf("exit %d", exit.ExitCode()), red
	default:
		return o.err.Error(), red
	}
}
