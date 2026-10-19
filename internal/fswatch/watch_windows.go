//go:build windows

package fswatch

import (
	"errors"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Watcher struct {
	handle     windows.Handle
	stop       windows.Handle
	ready      windows.Handle
	done       chan struct{}
	once       sync.Once
	overlapped *windows.Overlapped
	words      []uint32
}

const changeMask = windows.FILE_NOTIFY_CHANGE_FILE_NAME |
	windows.FILE_NOTIFY_CHANGE_DIR_NAME |
	windows.FILE_NOTIFY_CHANGE_SIZE |
	windows.FILE_NOTIFY_CHANGE_LAST_WRITE

func Watch(dir string, notify func(path string, overflow bool)) (*Watcher, error) {
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name,
		windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED,
		0)
	if err != nil {
		return nil, err
	}
	stop, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, err
	}
	ready, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(stop)
		windows.CloseHandle(handle)
		return nil, err
	}
	w := &Watcher{
		handle:     handle,
		stop:       stop,
		ready:      ready,
		done:       make(chan struct{}),
		overlapped: new(windows.Overlapped),
		words:      make([]uint32, 16*1024),
	}
	if err := w.arm(); err != nil {
		windows.CloseHandle(ready)
		windows.CloseHandle(stop)
		windows.CloseHandle(handle)
		return nil, err
	}
	go w.loop(dir, notify)
	return w, nil
}

func (w *Watcher) Close() {
	w.once.Do(func() {
		windows.SetEvent(w.stop)
		<-w.done
		windows.CloseHandle(w.stop)
	})
}

func (w *Watcher) Done() <-chan struct{} {
	return w.done
}

func (w *Watcher) buffer() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(&w.words[0])), len(w.words)*4)
}

func (w *Watcher) arm() error {
	windows.ResetEvent(w.ready)
	*w.overlapped = windows.Overlapped{HEvent: w.ready}
	buf := w.buffer()
	return windows.ReadDirectoryChanges(w.handle, &buf[0], uint32(len(buf)), true, changeMask, nil, w.overlapped, 0)
}

func (w *Watcher) loop(dir string, notify func(string, bool)) {
	defer close(w.done)
	defer windows.CloseHandle(w.handle)
	defer windows.CloseHandle(w.ready)

	buf := w.buffer()
	for {
		which, err := windows.WaitForMultipleObjects([]windows.Handle{w.ready, w.stop}, false, windows.INFINITE)
		if err != nil || which != windows.WAIT_OBJECT_0 {
			windows.CancelIoEx(w.handle, w.overlapped)
			var ignored uint32
			windows.GetOverlappedResult(w.handle, w.overlapped, &ignored, true)
			return
		}

		var size uint32
		if err := windows.GetOverlappedResult(w.handle, w.overlapped, &size, false); err != nil {
			notify("", true)
			if !errors.Is(err, windows.ERROR_NOTIFY_ENUM_DIR) {
				return
			}
		} else if size == 0 {
			notify("", true)
		} else {
			for offset := uint32(0); offset < size; {
				info := (*windows.FileNotifyInformation)(unsafe.Pointer(&buf[offset]))
				chars := unsafe.Slice(&info.FileName, info.FileNameLength/2)
				notify(filepath.Join(dir, windows.UTF16ToString(chars)), false)
				if info.NextEntryOffset == 0 {
					break
				}
				offset += info.NextEntryOffset
			}
		}

		if err := w.arm(); err != nil {
			notify("", true)
			return
		}
	}
}
