//go:build !windows

package account

import (
	"os"
	"strconv"
)

func currentID() (string, error) {
	return strconv.Itoa(os.Getuid()), nil
}
