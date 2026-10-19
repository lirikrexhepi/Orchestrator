package process

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"composer/internal/shell"
)

type Spec struct {
	Command       string
	Args          []string
	Cwd           string
	Env           map[string]string
	Stderr        func(line string)
	KillTree      bool
	DieWithParent bool
}

type Process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	mu         sync.Mutex
	exitErr    error
	exited     chan struct{}
	stderrTail []string
	killed     bool
	job        any
}

var ErrNotFound = errors.New("executable not found")

func Start(ctx context.Context, spec Spec) (*Process, error) {
	env := spec.Env
	if env == nil {
		env = shell.BaseEnvironment()
	}
	if _, ok := shell.LookPath(spec.Command, env); !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, spec.Command)
	}

	resolved := shell.SpawnCommand(spec.Command, spec.Args, env)
	cmd := exec.CommandContext(ctx, resolved.Command, resolved.Args...)
	cmd.Dir = spec.Cwd
	cmd.Env = shell.Slice(env)
	configureSysProcAttr(cmd)
	applyCommandLine(cmd, resolved)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	p := &Process{cmd: cmd, stdin: stdin, stdout: stdout, exited: make(chan struct{})}
	if spec.KillTree {
		attachJob(p, false)
	} else if spec.DieWithParent {
		attachJob(p, true)
	}

	go p.pumpStderr(stderr, spec.Stderr)
	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.exitErr = err
		p.mu.Unlock()
		if spec.DieWithParent {
			closeJob(p)
		}
		close(p.exited)
	}()

	return p, nil
}

const stderrTailLimit = 40

func (p *Process) pumpStderr(stderr io.ReadCloser, sink func(string)) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		p.mu.Lock()
		p.stderrTail = append(p.stderrTail, line)
		if len(p.stderrTail) > stderrTailLimit {
			p.stderrTail = p.stderrTail[len(p.stderrTail)-stderrTailLimit:]
		}
		p.mu.Unlock()
		if sink != nil {
			sink(line)
		}
	}
}

func (p *Process) Stdin() io.Writer      { return p.stdin }
func (p *Process) Stdout() io.Reader     { return p.stdout }
func (p *Process) PID() int              { return p.cmd.Process.Pid }
func (p *Process) Done() <-chan struct{} { return p.exited }

func (p *Process) StderrTail() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.stderrTail, "\n")
}

func (p *Process) ExitError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.killed {
		return nil
	}
	return p.exitErr
}

// Shutdown closes stdin so a well-behaved child can flush and exit, then
// escalates to a kill if it overstays the grace period.
func (p *Process) Shutdown(grace time.Duration) error {
	select {
	case <-p.exited:
		closeJob(p)
		return p.ExitError()
	default:
	}

	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()

	_ = p.stdin.Close()
	select {
	case <-p.exited:
		return nil
	case <-time.After(grace):
	}

	terminate(p.cmd)
	select {
	case <-p.exited:
		closeJob(p)
		return nil
	case <-time.After(2 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.exited
	}
	closeJob(p)
	return nil
}
