//go:build darwin

package macos

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
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
	flagDestroyRights      = 1 << 3
)

// The keys of an Environment.
const (
	// EnvPrompt is the text the password prompt shows.
	EnvPrompt = "prompt"
	// EnvUsername and EnvPassword give the credential without a prompt.
	EnvUsername = "username"
	EnvPassword = "password"
)

// Environment holds the items of an AuthorizationEnvironment.
type Environment map[string]string

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
// answers. env gives the prompt text or a credential. The authorization
// keeps the credential for the next check.
func (a *Authorization) Obtain(right string, interactive bool, env Environment) error {
	rights, keep, err := itemSet(map[string]string{right: ""})
	if err != nil {
		return err
	}
	environment, keepEnv, err := itemSet(env)
	if err != nil {
		return err
	}
	flags := uintptr(flagExtendRights)
	if interactive {
		flags |= flagInteractionAllowed
	}
	r, _, _ := syscall_syscall6(authorizationCopyRightsAddr, a.ref, uintptr(unsafe.Pointer(rights)), uintptr(unsafe.Pointer(environment)), flags, 0, 0)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(keepEnv)
	return check("AuthorizationCopyRights", r)
}

// itemSet builds an item set of names and values. An empty value is an
// item with no value, as a right is. The second result holds the Go
// memory that the set points to, for the caller to keep alive.
func itemSet(items map[string]string) (*authItemSet, []any, error) {
	if len(items) == 0 {
		return nil, nil, nil
	}
	list := make([]authItem, 0, len(items))
	keep := []any{}
	for name, value := range items {
		cname, err := syscall.BytePtrFromString(name)
		if err != nil {
			return nil, nil, err
		}
		item := authItem{name: cname}
		keep = append(keep, cname)
		if value != "" {
			data := []byte(value)
			item.value = unsafe.Pointer(&data[0])
			item.valueLength = uintptr(len(data))
			keep = append(keep, data)
		}
		list = append(list, item)
	}
	keep = append(keep, list)
	return &authItemSet{count: uint32(len(list)), items: &list[0]}, keep, nil
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
		if unix.BytePtrToString(item.name) != infoUsername || item.value == nil || item.valueLength == 0 {
			continue
		}
		value := unsafe.Slice((*byte)(item.value), item.valueLength)
		return string(bytes.TrimRight(value, "\x00")), nil
	}
	return "", errors.New("the authorization names no user")
}

// Free releases this reference to the authorization. A process that took
// the authorization from its external form keeps it.
func (a *Authorization) Free() {
	a.free(flagDefaults)
}

// Destroy releases the authorization and ends the credential it holds,
// also for every process that took it from its external form.
func (a *Authorization) Destroy() {
	a.free(flagDestroyRights)
}

func (a *Authorization) free(flags uintptr) {
	if a == nil || a.ref == 0 {
		return
	}
	_, _, _ = syscall_syscall(authorizationFreeAddr, a.ref, flags, 0)
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
