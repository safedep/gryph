//go:build darwin

package macos

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

//go:cgo_import_dynamic gryph_AuthorizationCreate AuthorizationCreate "/System/Library/Frameworks/Security.framework/Versions/A/Security"
//go:cgo_import_dynamic gryph_AuthorizationCopyRights AuthorizationCopyRights "/System/Library/Frameworks/Security.framework/Versions/A/Security"
//go:cgo_import_dynamic gryph_AuthorizationMakeExternalForm AuthorizationMakeExternalForm "/System/Library/Frameworks/Security.framework/Versions/A/Security"
//go:cgo_import_dynamic gryph_AuthorizationCreateFromExternalForm AuthorizationCreateFromExternalForm "/System/Library/Frameworks/Security.framework/Versions/A/Security"
//go:cgo_import_dynamic gryph_AuthorizationCopyInfo AuthorizationCopyInfo "/System/Library/Frameworks/Security.framework/Versions/A/Security"
//go:cgo_import_dynamic gryph_AuthorizationFreeItemSet AuthorizationFreeItemSet "/System/Library/Frameworks/Security.framework/Versions/A/Security"
//go:cgo_import_dynamic gryph_AuthorizationFree AuthorizationFree "/System/Library/Frameworks/Security.framework/Versions/A/Security"
//go:cgo_import_dynamic gryph_AuthorizationRightGet AuthorizationRightGet "/System/Library/Frameworks/Security.framework/Versions/A/Security"

var (
	authorizationCreateAddr                 uintptr
	authorizationCopyRightsAddr             uintptr
	authorizationMakeExternalFormAddr       uintptr
	authorizationCreateFromExternalFormAddr uintptr
	authorizationCopyInfoAddr               uintptr
	authorizationFreeItemSetAddr            uintptr
	authorizationFreeAddr                   uintptr
	authorizationRightGetAddr               uintptr
)

// The flags of Authorization Services (AuthorizationFlags).
const (
	flagDefaults           = 0
	flagInteractionAllowed = 1 << 0
	flagExtendRights       = 1 << 1
)

// The status codes of Authorization Services that a caller tells apart.
const (
	StatusDenied                = -60005
	StatusCanceled              = -60006
	StatusInteractionNotAllowed = -60007
)

// ExternalFormSize is the size of an AuthorizationExternalForm.
const ExternalFormSize = 32

// infoUsername is the tag of the user who authenticated, in the
// information of an authorization (kAuthorizationEnvironmentUsername).
const infoUsername = "username"

// OSStatus is a failed call of Authorization Services.
type OSStatus struct {
	Call   string
	Status int32
}

func (s OSStatus) Error() string {
	return fmt.Sprintf("%s: status %d", s.Call, s.Status)
}

// IsStatus reports whether err is the status code of a call.
func IsStatus(err error, status int32) bool {
	var s OSStatus
	return errors.As(err, &s) && s.Status == status
}

func check(call string, r uintptr) error {
	if status := int32(r); status != 0 {
		return OSStatus{Call: call, Status: status}
	}
	return nil
}

// authItem and authItemSet are AuthorizationItem and AuthorizationItemSet
// (also AuthorizationRights) on a 64-bit system.
type authItem struct {
	name        *byte
	valueLength uintptr
	value       unsafe.Pointer
	_           uint32 // flags, zero in a request
	_           uint32
}

type authItemSet struct {
	count uint32
	_     uint32
	items *authItem
}

// Authorization is an AuthorizationRef. Free releases it.
type Authorization struct {
	ref uintptr
}

// NewAuthorization makes an authorization of this process with no
// rights.
func NewAuthorization() (*Authorization, error) {
	var ref uintptr
	r, _, _ := syscall_syscall6(authorizationCreateAddr, 0, 0, flagDefaults, uintptr(unsafe.Pointer(&ref)), 0, 0)
	if err := check("AuthorizationCreate", r); err != nil {
		return nil, err
	}
	return &Authorization{ref: ref}, nil
}

// AuthorizationFromExternal takes the authorization that another process
// made and passed in its external form.
func AuthorizationFromExternal(form [ExternalFormSize]byte) (*Authorization, error) {
	var ref uintptr
	r, _, _ := syscall_syscall(authorizationCreateFromExternalFormAddr, uintptr(unsafe.Pointer(&form)), uintptr(unsafe.Pointer(&ref)), 0)
	if err := check("AuthorizationCreateFromExternalForm", r); err != nil {
		return nil, err
	}
	return &Authorization{ref: ref}, nil
}

// Obtain gets right for the authorization, by the rule of the right in
// the policy database. With interactive the system may ask the person in
// the login session of this process, and the call lasts until the person
// answers. The authorization keeps the credential for the next check.
func (a *Authorization) Obtain(right string, interactive bool) error {
	name, err := syscall.BytePtrFromString(right)
	if err != nil {
		return err
	}
	item := authItem{name: name}
	rights := authItemSet{count: 1, items: &item}
	flags := uintptr(flagExtendRights)
	if interactive {
		flags |= flagInteractionAllowed
	}
	r, _, _ := syscall_syscall6(authorizationCopyRightsAddr, a.ref, uintptr(unsafe.Pointer(&rights)), 0, flags, 0, 0)
	runtime.KeepAlive(name)
	runtime.KeepAlive(&item)
	return check("AuthorizationCopyRights", r)
}

// External returns the external form of the authorization, which another
// process turns back into the same authorization while this one holds it.
func (a *Authorization) External() ([ExternalFormSize]byte, error) {
	var form [ExternalFormSize]byte
	r, _, _ := syscall_syscall(authorizationMakeExternalFormAddr, a.ref, uintptr(unsafe.Pointer(&form)), 0)
	return form, check("AuthorizationMakeExternalForm", r)
}

// Username returns the name of the user who authenticated for the
// authorization.
func (a *Authorization) Username() (string, error) {
	tag, err := syscall.BytePtrFromString(infoUsername)
	if err != nil {
		return "", err
	}
	var info *authItemSet
	r, _, _ := syscall_syscall(authorizationCopyInfoAddr, a.ref, uintptr(unsafe.Pointer(tag)), uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(tag)
	if err := check("AuthorizationCopyInfo", r); err != nil {
		return "", err
	}
	if info == nil {
		return "", errors.New("the authorization names no user")
	}
	defer func() { _, _, _ = syscall_syscall(authorizationFreeItemSetAddr, uintptr(unsafe.Pointer(info)), 0, 0) }()
	if info.count == 0 || info.items == nil {
		return "", errors.New("the authorization names no user")
	}
	for _, item := range unsafe.Slice(info.items, info.count) {
		if item.value == nil || item.valueLength == 0 {
			continue
		}
		value := unsafe.Slice((*byte)(item.value), item.valueLength)
		return string(bytes.TrimRight(value, "\x00")), nil
	}
	return "", errors.New("the authorization names no user")
}

// Free releases the authorization. The credential it holds goes with it.
func (a *Authorization) Free() {
	if a == nil || a.ref == 0 {
		return
	}
	_, _, _ = syscall_syscall(authorizationFreeAddr, a.ref, flagDefaults, 0)
	a.ref = 0
}

// RightExists reports whether the policy database of the host defines
// right. A right that is not there falls back to the default rule of the
// database, which asks an administrator, so a caller checks first.
func RightExists(right string) bool {
	name, err := syscall.BytePtrFromString(right)
	if err != nil {
		return false
	}
	r, _, _ := syscall_syscall(authorizationRightGetAddr, uintptr(unsafe.Pointer(name)), 0, 0)
	runtime.KeepAlive(name)
	return int32(r) == 0
}
