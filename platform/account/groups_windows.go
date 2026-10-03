package account

import "errors"

// ErrGroupsUnsupported says the platform has no group database that the
// service reads.
var ErrGroupsUnsupported = errors.New("account: group membership is not supported on this platform")

func memberOf(_, _ string) (bool, error) {
	return false, ErrGroupsUnsupported
}
