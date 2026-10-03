package procs

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func list() ([]Process, error) {
	me, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var out []Process
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		p, ok := describe(entry, me.User.Sid)
		if ok {
			out = append(out, p)
		}
	}
	if err != nil && !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return out, fmt.Errorf("list processes: %w", err)
	}
	return out, nil
}

// describe returns the process of an entry when the current account owns
// it. A process that cannot be opened belongs to another account or to the
// system.
func describe(entry windows.ProcessEntry32, me *windows.SID) (Process, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
	if err != nil {
		return Process{}, false
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var token windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &token); err != nil {
		return Process{}, false
	}
	defer func() { _ = token.Close() }()
	owner, err := token.GetTokenUser()
	if err != nil || !owner.User.Sid.Equals(me) {
		return Process{}, false
	}

	p := Process{PID: int(entry.ProcessID), Name: windows.UTF16ToString(entry.ExeFile[:])}
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err == nil {
		p.Started = time.Unix(0, creation.Nanoseconds())
	}
	return p, true
}
