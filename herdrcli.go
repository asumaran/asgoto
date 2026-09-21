package main

// Running the herdr CLI. A server that does not answer is an error to report,
// never a hang, and a failure is said the way herdr said it.
//
// This file is the same in every tool of the family that runs herdr.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

const (
	// herdrTimeout bounds one read from the herdr CLI (the popup and -dump
	// alike).
	herdrTimeout = 5 * time.Second
	// herdrActionTimeout bounds a command that changes something: opening a
	// worktree may create a space, which takes longer than a read.
	herdrActionTimeout = 30 * time.Second
)

// herdrRun runs a herdr command that reads and returns its stdout.
func herdrRun(args ...string) ([]byte, error) { return herdrExec(herdrTimeout, args...) }

// herdrAct runs a herdr command that changes something and returns its stdout
// (the pane it made, say).
func herdrAct(args ...string) ([]byte, error) { return herdrExec(herdrActionTimeout, args...) }

// herdrDo is herdrAct for a command whose answer is not needed.
func herdrDo(args ...string) error {
	_, err := herdrAct(args...)
	return err
}

func herdrExec(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, herdrBin(), args...)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("no answer from herdr after %s", timeout)
	}
	var stderr []byte
	if ee, ok := err.(*exec.ExitError); ok {
		stderr = ee.Stderr
	}
	return nil, herdrError(err, out, stderr)
}

// herdrError is err said the way herdr said it: the CLI reports a failure as
// a JSON error object (on either stream), whose message beats "exit status 1".
func herdrError(err error, outputs ...[]byte) error {
	for _, o := range outputs {
		var resp struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(bytes.TrimSpace(o), &resp) == nil && resp.Error != nil && resp.Error.Message != "" {
			return fmt.Errorf("%s", resp.Error.Message)
		}
	}
	return err
}
