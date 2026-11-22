//go:build darwin

package antigravity

import (
	"fmt"
	"os/exec"
	"strings"
)

type osKeyring struct{}

func newOSKeyring() Keyring {
	return &osKeyring{}
}

func (k *osKeyring) Get(service, account string) (string, error) {
	cmd := exec.Command("/usr/bin/security", "find-generic-password", "-s", service, "-a", account, "-w")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("security find-generic-password: %w", err)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

func (k *osKeyring) Set(service, account, secret string) error {
	cmd := exec.Command("/usr/bin/security", "add-generic-password", "-U", "-s", service, "-a", account, "-w", secret)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("security add-generic-password: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (k *osKeyring) Delete(service, account string) error {
	cmd := exec.Command("/usr/bin/security", "delete-generic-password", "-s", service, "-a", account)
	_ = cmd.Run()
	return nil
}
