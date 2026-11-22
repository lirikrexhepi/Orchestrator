//go:build windows

package antigravity

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	advapi32        = windows.NewLazySystemDLL("advapi32.dll")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

type winCredential struct {
	flags              uint32
	credType           uint32
	targetName         *uint16
	comment            *uint16
	lastWritten        windows.Filetime
	credentialBlobSize uint32
	_                  uint32 // 64-bit alignment padding
	credentialBlob     *byte
	persist            uint32
	attributeCount     uint32
	attributes         uintptr
	targetAlias        *uint16
	userName           *uint16
}

type osKeyring struct{}

func newOSKeyring() Keyring {
	return &osKeyring{}
}

func (k *osKeyring) target(service, account string) string {
	return service + ":" + account
}

func (k *osKeyring) Get(service, account string) (string, error) {
	targetPtr, err := windows.UTF16PtrFromString(k.target(service, account))
	if err != nil {
		return "", err
	}

	var credPtr *winCredential
	r, _, errNo := procCredReadW.Call(
		uintptr(unsafe.Pointer(targetPtr)),
		uintptr(credTypeGeneric),
		0,
		uintptr(unsafe.Pointer(&credPtr)),
	)
	if r == 0 {
		return "", fmt.Errorf("CredReadW: %w", errNo)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credPtr)))

	if credPtr.credentialBlobSize == 0 || credPtr.credentialBlob == nil {
		return "", nil
	}
	blob := unsafe.Slice(credPtr.credentialBlob, credPtr.credentialBlobSize)
	return string(blob), nil
}

func (k *osKeyring) Set(service, account, secret string) error {
	targetPtr, err := windows.UTF16PtrFromString(k.target(service, account))
	if err != nil {
		return err
	}
	userPtr, err := windows.UTF16PtrFromString(account)
	if err != nil {
		return err
	}

	blob := []byte(secret)
	var blobPtr *byte
	if len(blob) > 0 {
		blobPtr = &blob[0]
	}

	cred := winCredential{
		credType:           credTypeGeneric,
		targetName:         targetPtr,
		userName:           userPtr,
		credentialBlobSize: uint32(len(blob)),
		credentialBlob:     blobPtr,
		persist:            credPersistLocalMachine,
	}

	r, _, errNo := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if r == 0 {
		return fmt.Errorf("CredWriteW: %w", errNo)
	}
	return nil
}

func (k *osKeyring) Delete(service, account string) error {
	targetPtr, err := windows.UTF16PtrFromString(k.target(service, account))
	if err != nil {
		return err
	}

	r, _, errNo := procCredDeleteW.Call(
		uintptr(unsafe.Pointer(targetPtr)),
		uintptr(credTypeGeneric),
		0,
	)
	if r == 0 && errNo != windows.ERROR_NOT_FOUND {
		return fmt.Errorf("CredDeleteW: %w", errNo)
	}
	return nil
}
