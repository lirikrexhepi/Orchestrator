//go:build !windows && !darwin && !linux

package antigravity

import (
	"fmt"
	"sync"
)

type osKeyring struct {
	mu    sync.Mutex
	store map[string]map[string]string
}

func newOSKeyring() Keyring {
	return &osKeyring{
		store: make(map[string]map[string]string),
	}
}

func (k *osKeyring) Get(service, account string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if accs, ok := k.store[service]; ok {
		if sec, ok := accs[account]; ok {
			return sec, nil
		}
	}
	return "", fmt.Errorf("secret not found for %s/%s", service, account)
}

func (k *osKeyring) Set(service, account, secret string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.store[service]; !ok {
		k.store[service] = make(map[string]string)
	}
	k.store[service][account] = secret
	return nil
}

func (k *osKeyring) Delete(service, account string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if accs, ok := k.store[service]; ok {
		delete(accs, account)
	}
	return nil
}
