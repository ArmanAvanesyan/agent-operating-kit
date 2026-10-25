//go:build windows

package platformfs

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"unsafe"
)

func currentSID() (*windows.SID, error) {
	token := windows.GetCurrentProcessToken()
	u, e := token.GetTokenUser()
	if e != nil {
		return nil, e
	}
	return u.User.Sid, nil
}
func PrivateDirectory(path string) error {
	st, e := os.Lstat(path)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("private non-reparse directory required")
	}
	sid, e := currentSID()
	if e != nil {
		return e
	}
	sd, e := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		return e
	}
	owner, _, e := sd.Owner()
	if e != nil || !owner.Equals(sid) {
		return errors.New("directory SID mismatch")
	}
	acl, _, e := sd.DACL()
	if e != nil || acl == nil {
		return errors.New("private DACL required")
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var raw *windows.ACCESS_ALLOWED_ACE
		if e = windows.GetAce(acl, i, &raw); e != nil {
			return e
		}
		if raw.Header.AceType == windows.ACCESS_ALLOWED_ACE_TYPE {
			allowed := (*windows.SID)(unsafe.Pointer(&raw.SidStart))
			if !allowed.Equals(sid) && allowed.String() != "S-1-5-18" && allowed.String() != "S-1-5-32-544" {
				return errors.New("broad directory access rejected")
			}
		} else if raw.Header.AceType != windows.ACCESS_DENIED_ACE_TYPE {
			return errors.New("unsupported access-control entry")
		}
	}
	return nil
}
func ProtectNewDirectory(path string) error {
	sid, e := currentSID()
	if e != nil {
		return e
	}
	sd, e := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + sid.String() + ")(A;OICI;FA;;;SY)")
	if e != nil {
		return e
	}
	acl, _, e := sd.DACL()
	if e != nil {
		return e
	}
	if e = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); e != nil {
		return e
	}
	return PrivateDirectory(path)
}
