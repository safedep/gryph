//go:build !linux

package kernel

func setting(string) (string, error) {
	return "", ErrUnsupported
}
