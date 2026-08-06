//go:build windows

package secretfile

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestCheckPermissionsRejectsBroadWindowsDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := Protect(file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	currentUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal(err)
	}
	secureEntries := []windows.EXPLICIT_ACCESS{
		allowWindowsAccess(currentUser.User.Sid, windows.GENERIC_ALL),
		allowWindowsAccess(system, windows.GENERIC_ALL),
		allowWindowsAccess(administrators, windows.GENERIC_ALL),
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err = os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckPermissions(file, info); err != nil {
		_ = file.Close()
		t.Fatalf("private DACL rejected: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	setWindowsDACL(t, path, append(secureEntries, allowWindowsAccess(everyone, windows.GENERIC_READ)))
	file, err = os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := CheckPermissions(file, info); err == nil {
		t.Fatal("DACL granting Everyone read access was accepted")
	}
}

func allowWindowsAccess(sid *windows.SID, permissions windows.ACCESS_MASK) windows.EXPLICIT_ACCESS {
	return windows.EXPLICIT_ACCESS{
		AccessPermissions: permissions,
		AccessMode:        windows.GRANT_ACCESS,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}
}

func setWindowsDACL(t *testing.T, path string, entries []windows.EXPLICIT_ACCESS) {
	t.Helper()
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		t.Fatal(err)
	}
}
