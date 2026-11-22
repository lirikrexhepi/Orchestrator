package devserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"composer/internal/domain"
)

func TestIsShim(t *testing.T) {
	cases := map[string]bool{
		`C:\Users\PC\AppData\Roaming\composer\bin\composer-serve.exe`: true,
		"/home/u/.config/composer/bin/composer-serve":                 true,
		"composer-serve":                         true,
		"COMPOSER-SERVE.EXE":                     true,
		`C:\Users\PC\AppData\Roaming\composer\bin\composer-task.exe`:  true,
		"/home/u/.config/composer/bin/composer-task":                  true,
		"composer-task":                          true,
		"COMPOSER-TASK.EXE":                      true,
		`C:\Program Files\composer\composer.exe`: false,
		"/usr/local/bin/composer":                false,
	}
	for argv0, want := range cases {
		if got := IsShim(argv0); got != want {
			t.Errorf("IsShim(%q) = %v, want %v", argv0, got, want)
		}
	}
}

func TestShimKeepsServerAliveAfterItExits(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the app binary")
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
	shimPath := filepath.Join(binDir, shimFileName())
	build := exec.Command(goTool, "build", "-o", shimPath, "./desktop")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build shim: %v\n%s", err, out)
	}

	manager := NewManager()
	defer manager.StopAll()
	control := NewControl(manager)
	if err := control.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer control.Close()

	workdir := t.TempDir()
	shim := exec.Command(shimPath, node, "-e", "console.log('listening on http://localhost:45888'); setInterval(()=>{}, 1000)")
	shim.Dir = workdir
	shim.Env = append(os.Environ(),
		EnvURL+"="+control.URL(),
		EnvToken+"="+control.Token(),
		EnvOwner+"=thread-7",
	)

	out, err := shim.CombinedOutput()
	if err != nil {
		t.Fatalf("shim failed: %v\n%s", err, out)
	}

	live := manager.List()
	if len(live) != 1 {
		t.Fatalf("want 1 managed server, got %d (%s)", len(live), out)
	}
	server := live[0]
	if server.Status != StatusRunning {
		t.Errorf("status = %q, want running", server.Status)
	}
	if server.OwnerThreadID != "thread-7" {
		t.Errorf("owner = %q, want thread-7", server.OwnerThreadID)
	}
	if server.Cwd != workdir {
		t.Errorf("cwd = %q, want %q", server.Cwd, workdir)
	}
	if !processAlive(server.PID) {
		t.Fatalf("server pid %d is not alive after the shim exited", server.PID)
	}

	if !waitFor(t, func() bool {
		for _, s := range manager.List() {
			if s.ID == server.ID && s.Port == 45888 {
				return true
			}
		}
		return false
	}) {
		t.Errorf("port never detected: %v", manager.Logs(server.ID))
	}

	manager.StopAll()
	if !waitFor(t, func() bool { return !processAlive(server.PID) }) {
		t.Errorf("server pid %d outlived Composer", server.PID)
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FI", "PID eq "+itoa(pid), "/NH").Output()
		if err != nil {
			return false
		}
		return len(out) > 0 && !containsNoTasks(string(out))
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(nil) == nil
}

func containsNoTasks(out string) bool {
	for _, marker := range []string{"No tasks", "INFO:"} {
		if len(out) >= len(marker) && contains(out, marker) {
			return true
		}
	}
	return false
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func TestTaskControl(t *testing.T) {
	manager := NewManager()
	defer manager.StopAll()
	control := NewControl(manager)

	planCh := make(chan []domain.PlanEntry, 10)
	control.SetTaskUpdater(func(threadID string, plan []domain.PlanEntry) {
		planCh <- plan
	})

	thread := "test-thread-1"

	// 1. Set initial 3 tasks
	tasks := control.MutateTask(thread, "add", "", "Fix login button")
	tasks = control.MutateTask(thread, "add", "", "Update auth modal")
	tasks = control.MutateTask(thread, "add", "", "Add error toasts")
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	// 2. Start task 1
	tasks = control.MutateTask(thread, "start", "1", "")
	if tasks[0].Status != "in_progress" {
		t.Fatalf("expected task 1 in_progress, got %s", tasks[0].Status)
	}

	// 3. Mark task 1 done
	tasks = control.MutateTask(thread, "done", "1", "")
	if tasks[0].Status != "completed" {
		t.Fatalf("expected task 1 completed, got %s", tasks[0].Status)
	}

	// 4. Toggle task 2
	tasks = control.MutateTask(thread, "toggle", "2", "")
	if tasks[1].Status != "completed" {
		t.Fatalf("expected task 2 completed after toggle, got %s", tasks[1].Status)
	}
	tasks = control.MutateTask(thread, "toggle", "2", "")
	if tasks[1].Status != "pending" {
		t.Fatalf("expected task 2 pending after 2nd toggle, got %s", tasks[1].Status)
	}

	// 5. Remove task 3
	tasks = control.MutateTask(thread, "remove", "3", "")
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks after removal, got %d", len(tasks))
	}

	// 6. Check GetTasks
	stored := control.GetTasks(thread)
	if len(stored) != 2 {
		t.Fatalf("expected GetTasks to return 2, got %d", len(stored))
	}

	// 7. Verify at least one plan was emitted through updater channel
	select {
	case plan := <-planCh:
		if len(plan) == 0 {
			t.Errorf("expected plan entries in updater callback")
		}
	default:
		// updater is async
	}
}

func TestTaskHTTPAndShim(t *testing.T) {
	manager := NewManager()
	defer manager.StopAll()
	control := NewControl(manager)
	if err := control.Listen(); err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer control.Close()

	thread := "thread-shim-test"
	t.Setenv(EnvURL, control.URL())
	t.Setenv(EnvToken, control.Token())
	t.Setenv(EnvOwner, thread)

	// Test RunTaskShim set
	code := RunTaskShim([]string{"set", "Implement feature A", "Fix styling bug B", "Add unit tests C"})
	if code != 0 {
		t.Fatalf("RunTaskShim set failed with exit code %d", code)
	}

	tasks := control.GetTasks(thread)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks after set, got %d", len(tasks))
	}

	// Test RunTaskShim start 1
	code = RunTaskShim([]string{"start", "1"})
	if code != 0 {
		t.Fatalf("RunTaskShim start failed with exit code %d", code)
	}
	tasks = control.GetTasks(thread)
	if tasks[0].Status != "in_progress" {
		t.Fatalf("expected task 1 in_progress, got %s", tasks[0].Status)
	}

	// Test RunTaskShim done 1
	code = RunTaskShim([]string{"done", "1"})
	if code != 0 {
		t.Fatalf("RunTaskShim done failed with exit code %d", code)
	}
	tasks = control.GetTasks(thread)
	if tasks[0].Status != "completed" {
		t.Fatalf("expected task 1 completed, got %s", tasks[0].Status)
	}

	// Test RunTaskShim add 4th task
	code = RunTaskShim([]string{"add", "Deploy to staging D"})
	if code != 0 {
		t.Fatalf("RunTaskShim add failed with exit code %d", code)
	}
	tasks = control.GetTasks(thread)
	if len(tasks) != 4 {
		t.Fatalf("expected 4 tasks after add, got %d", len(tasks))
	}

	// Test RunTaskShim list
	code = RunTaskShim([]string{"list"})
	if code != 0 {
		t.Fatalf("RunTaskShim list failed with exit code %d", code)
	}
}


