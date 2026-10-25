//go:build windows

package credentialbroker

import (
	"context"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"golang.org/x/sys/windows"
	"unsafe"
)

var credDLL = windows.NewLazySystemDLL("advapi32.dll")
var credRead = credDLL.NewProc("CredReadW")
var credWrite = credDLL.NewProc("CredWriteW")
var credFree = credDLL.NewProc("CredFree")

type credential struct {
	Flags, Type             uint32
	TargetName, Comment     *uint16
	LastWritten             windows.Filetime
	BlobSize                uint32
	Blob                    *byte
	Persist, AttributeCount uint32
	Attributes              uintptr
	TargetAlias, UserName   *uint16
}
type winStore struct{ namespace string }

func openNative(c a.BrokerConfig) (Store, error) { return winStore{c.Namespace}, nil }
func (s winStore) Read(ctx context.Context, key string) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	name, e := windows.UTF16PtrFromString("WorkKit/" + s.namespace + "/" + key)
	if e != nil {
		return nil, e
	}
	var value *credential
	ok, _, err := credRead.Call(uintptr(unsafe.Pointer(name)), 1, 0, uintptr(unsafe.Pointer(&value)))
	if ok == 0 {
		if err == windows.ERROR_NOT_FOUND {
			return nil, ErrAbsent
		}
		return nil, errors.New("credential manager unavailable")
	}
	defer credFree.Call(uintptr(unsafe.Pointer(value)))
	if value.BlobSize > 2560 || value.Blob == nil {
		return nil, errors.New("credential blob invalid")
	}
	return append([]byte{}, unsafe.Slice(value.Blob, int(value.BlobSize))...), nil
}
func (s winStore) Write(ctx context.Context, key string, b []byte) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if len(b) == 0 || len(b) > 2560 {
		return errors.New("credential manager blob bound")
	}
	name, e := windows.UTF16PtrFromString("WorkKit/" + s.namespace + "/" + key)
	if e != nil {
		return e
	}
	user, _ := windows.UTF16PtrFromString("WorkKit")
	value := credential{Type: 1, TargetName: name, BlobSize: uint32(len(b)), Blob: &b[0], Persist: 2, UserName: user}
	ok, _, _ := credWrite.Call(uintptr(unsafe.Pointer(&value)), 0)
	if ok == 0 {
		return errors.New("credential manager persistence failed")
	}
	return nil
}
