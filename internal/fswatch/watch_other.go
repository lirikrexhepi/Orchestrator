//go:build !windows

package fswatch

import "errors"

type Watcher struct {
	done chan struct{}
}

func Watch(dir string, notify func(path string, overflow bool)) (*Watcher, error) {
	return nil, errors.New("directory watching is not supported on this platform")
}

func (w *Watcher) Close() {}

func (w *Watcher) Done() <-chan struct{} {
	return w.done
}
