//go:build linux

package antigravity

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

type osKeyring struct {
	fallbackMu sync.Mutex
	fallback   map[string]map[string]string
}

func newOSKeyring() Keyring {
	return &osKeyring{
		fallback: make(map[string]map[string]string),
	}
}

func (k *osKeyring) Get(service, account string) (string, error) {
	if _, err := exec.LookPath("secret-tool"); err == nil {
		cmd := exec.Command("secret-tool", "lookup", "service", service, "account", account)
		out, err := cmd.Output()
		if err == nil {
			return strings.TrimRight(string(out), "\r\n"), nil
		}
	}

	k.fallbackMu.Lock()
	defer k.fallbackMu.Unlock()
	if accs, ok := k.fallback[service]; ok {
		if sec, ok := accs[account]; ok {
			return sec, nil
		}
	}
	return "", fmt.Errorf("secret not found for %s/%s", service, account)
}

func (k *osKeyring) Set(service, account, secret string) error {
	if _, err := exec.LookPath("secret-tool"); err == nil {
		cmd := exec.Command("secret-tool", "store", "--label="+service, "service", service, "account", account)
		cmd.Stdin = strings.NewReader(secret)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	k.fallbackMu.Lock()
	defer k.fallbackMu.Unlock()
	if _, ok := k.fallback[service]; !ok {
		k.fallback[service] = make(map[string]string)
	}
	k.fallback[service][account] = secret
	return nil
}

func (k *osKeyring) Delete(service, account string) error {
	if _, err := exec.LookPath("secret-tool"); err == nil {
		_ = exec.Command("secret-tool", "clear", "service", service, "account", account).Run()
	}

	k.fallbackMu.Lock()
	defer k.fallbackMu.Unlock()
	if accs, ok := k.fallback[service]; ok {
		delete(accs, account)
	}
	return nil
}
