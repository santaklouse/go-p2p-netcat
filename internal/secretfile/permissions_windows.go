//go:build windows

package secretfile

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procReOpenFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

const secretAccessMask = windows.ACCESS_MASK(
	windows.GENERIC_ALL |
		windows.GENERIC_READ |
		windows.GENERIC_WRITE |
		windows.FILE_READ_DATA |
		windows.FILE_WRITE_DATA |
		windows.FILE_APPEND_DATA |
		windows.DELETE |
		windows.WRITE_DAC |
		windows.WRITE_OWNER,
)

// CheckPermissions permits access-control entries only for the file owner,
// the current process user, LocalSystem, and built-in Administrators. Unknown
// DACL entry types fail closed because their effective access cannot be proven
// private by this checker.
func CheckPermissions(file *os.File, _ os.FileInfo) error {
	descriptor, err := windows.GetSecurityInfo(
		windows.Handle(file.Fd()),
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("inspect Windows DACL: %w", err)
	}
	if descriptor == nil || !descriptor.IsValid() {
		return errors.New("Windows security descriptor is missing or invalid")
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.IsValid() {
		return errors.New("Windows file owner is missing or invalid")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return errors.New("Windows file DACL is missing or permits unrestricted access")
	}
	currentUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("inspect current Windows user: %w", err)
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return fmt.Errorf("inspect Windows DACL entry %d: %w", index, err)
		}
		if ace == nil {
			return fmt.Errorf("Windows DACL entry %d is missing", index)
		}
		switch ace.Header.AceType {
		case windows.ACCESS_DENIED_ACE_TYPE:
			continue
		case windows.ACCESS_ALLOWED_ACE_TYPE:
		default:
			return fmt.Errorf("Windows DACL entry %d has unsupported type %d", index, ace.Header.AceType)
		}
		if ace.Mask&secretAccessMask == 0 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if sid == nil || !sid.IsValid() {
			return fmt.Errorf("Windows DACL entry %d has an invalid SID", index)
		}
		if sid.Equals(owner) || sid.Equals(currentUser.User.Sid) ||
			sid.IsWellKnown(windows.WinLocalSystemSid) ||
			sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) ||
			sid.IsWellKnown(windows.WinCreatorOwnerSid) {
			continue
		}
		return fmt.Errorf("Windows DACL grants secret access to %s", sid.String())
	}
	return nil
}

// Protect replaces inherited access with a protected DACL for the current
// user, LocalSystem, and built-in Administrators.
func Protect(file *os.File) error {
	currentUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("inspect current Windows user: %w", err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("create LocalSystem SID: %w", err)
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return fmt.Errorf("create Administrators SID: %w", err)
	}
	entries := []windows.EXPLICIT_ACCESS{
		allowAccess(currentUser.User.Sid, windows.GENERIC_ALL),
		allowAccess(system, windows.GENERIC_ALL),
		allowAccess(administrators, windows.GENERIC_ALL),
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return fmt.Errorf("build private Windows DACL: %w", err)
	}
	securityHandle, err := reopenForDACL(file)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(securityHandle)
	if err := windows.SetSecurityInfo(
		securityHandle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		return fmt.Errorf("set private Windows DACL: %w", err)
	}
	if err := CheckPermissions(file, nil); err != nil {
		return fmt.Errorf("verify private Windows DACL: %w", err)
	}
	return nil
}

// reopenForDACL obtains WRITE_DAC for the same kernel file object instead of
// resolving file.Name again. The regular os.File handle intentionally lacks
// WRITE_DAC, which SetSecurityInfo requires when replacing a DACL.
func reopenForDACL(file *os.File) (windows.Handle, error) {
	handle, _, callErr := procReOpenFile.Call(
		file.Fd(),
		uintptr(windows.READ_CONTROL|windows.WRITE_DAC),
		uintptr(windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE),
		uintptr(windows.FILE_FLAG_OPEN_REPARSE_POINT),
	)
	securityHandle := windows.Handle(handle)
	if securityHandle == windows.InvalidHandle {
		return windows.InvalidHandle, fmt.Errorf("reopen Windows file with WRITE_DAC: %w", callErr)
	}
	return securityHandle, nil
}

func allowAccess(sid *windows.SID, permissions windows.ACCESS_MASK) windows.EXPLICIT_ACCESS {
	return windows.EXPLICIT_ACCESS{
		AccessPermissions: permissions,
		AccessMode:        windows.GRANT_ACCESS,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
}
