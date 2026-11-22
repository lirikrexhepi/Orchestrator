package devserver

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"composer/internal/servers"
)

func TestAgentStartsDurableServerThroughLauncher(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real agent CLI")
	}
	agy, err := exec.LookPath("agy")
	if err != nil {
		t.Skip("agy CLI not installed")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not available")
	}
	node := nodePath(t)

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	build := exec.Command(goTool, "build", "-o", filepath.Join(binDir, shimFileName()), "./desktop")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build shim: %v\n%s", err, out)
	}

	project := t.TempDir()
	writeFile(t, filepath.Join(project, "package.json"), `{"name":"probe","scripts":{"dev":"node server.js"}}`)
	writeFile(t, filepath.Join(project, "server.js"),
		"require('http').createServer((_,r)=>r.end('ok')).listen(45777,()=>console.log('listening on http://localhost:45777'));")

	manager := NewManager()
	defer manager.StopAll()
	control := NewControl(manager)
	if err := control.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer control.Close()

	prompt := "Start the dev server for this project.\n\n" + Brief()
	cmd := exec.Command(agy,
		"--print", prompt,
		"--output-format", "stream-json",
		"--add-dir", project,
		"--print-timeout", "3m",
		"--mode", "accept-edits",
		"--dangerously-skip-permissions",
	)
	cmd.Dir = project
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		EnvURL+"="+control.URL(),
		EnvToken+"="+control.Token(),
		"NODE_PATH="+filepath.Dir(node),
	)

	var agentPID atomic.Int64
	control.SetOwnerResolver(func(callerPID int) string {
		pid := int(agentPID.Load())
		if pid == 0 {
			return ""
		}
		owner, ok := servers.OwnerOfPID(context.Background(), callerPID,
			[]servers.Owner{{ThreadID: "thread-agent", Title: "Run dev server", PID: pid}})
		if !ok {
			return ""
		}
		return owner.ThreadID
	})

	var buffer bytes.Buffer
	cmd.Stdout = &buffer
	cmd.Stderr = &buffer
	if err := cmd.Start(); err != nil {
		t.Fatalf("start agy: %v", err)
	}
	agentPID.Store(int64(cmd.Process.Pid))
	err = cmd.Wait()
	out := buffer.Bytes()
	t.Logf("agy exit=%v", err)

	live := manager.List()
	if len(live) == 0 {
		t.Fatalf("agent never used %s; output:\n%s", ShimName, tail(string(out), 2000))
	}

	server := live[0]
	t.Logf("managed server: cmd=%q cwd=%q pid=%d owner=%q", server.Command, server.Cwd, server.PID, server.OwnerThreadID)
	if server.OwnerThreadID != "thread-agent" {
		t.Errorf("owner = %q, want thread-agent", server.OwnerThreadID)
	}
	t.Logf("status=%v exit=%v port=%d", server.Status, server.ExitCode, server.Port)
	for _, line := range manager.Logs(server.ID) {
		t.Logf("  log: %s", line)
	}
	if !processAlive(server.PID) {
		t.Errorf("server pid %d died with the agent turn", server.PID)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tail(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return "..." + text[len(text)-limit:]
}
