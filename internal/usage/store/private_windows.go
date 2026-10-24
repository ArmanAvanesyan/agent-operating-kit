//go:build windows

package store

import (
	"errors"
	"golang.org/x/sys/windows"
)

func restrict(path string) error {
	u, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		return e
	}
	sd, e := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if e != nil {
		return e
	}
	owner, _, e := sd.Owner()
	if e != nil {
		return e
	}
	if !owner.Equals(u.User.Sid) {
		return errors.New("accounting storage must be owned by current user")
	}
	desired, e := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + u.User.Sid.String() + ")")
	if e != nil {
		return e
	}
	acl, _, e := desired.DACL()
	if e != nil {
		return e
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
func secureDirectory(path string) error { return restrict(path) }
func secureFile(path string) error      { return restrict(path) }
