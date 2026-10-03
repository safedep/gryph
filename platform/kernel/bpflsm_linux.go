package kernel

import (
	"bufio"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The sources of the probe. Tests replace them.
var (
	lsmListPath = "/sys/kernel/security/lsm"
	btfPath     = "/sys/kernel/btf/vmlinux"
	configPaths = func() []string {
		release, _ := os.ReadFile("/proc/sys/kernel/osrelease")
		return []string{"/proc/config.gz", filepath.Join("/boot", "config-"+strings.TrimSpace(string(release)))}
	}
)

func probeBPFLSM() (BPFLSM, error) {
	var b BPFLSM
	if data, err := os.ReadFile(lsmListPath); err == nil {
		for _, name := range strings.Split(strings.TrimSpace(string(data)), ",") {
			if name != "" {
				b.Active = append(b.Active, name)
			}
		}
		if b.Active == nil {
			b.Active = []string{}
		}
		for _, name := range b.Active {
			if name == "bpf" {
				b.BPFActive = true
			}
		}
	}
	if _, err := os.Stat(btfPath); err == nil {
		b.BTF = true
	}
	for _, path := range configPaths() {
		value, ok := configValue(path, "CONFIG_BPF_LSM")
		if !ok {
			continue
		}
		b.Built = value
		b.ConfigSource = path
		break
	}
	return b, nil
}

// configValue reads one option of a kernel configuration file, plain or
// gzip. It returns "n" for an option the file lists as not set, the
// value for a set option, and false when the file is not readable or has
// no line for the option.
func configValue(path, option string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return "", false
		}
		defer func() { _ = gz.Close() }()
		r = gz
	}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, option+"=") {
			return strings.TrimPrefix(line, option+"="), true
		}
		if line == "# "+option+" is not set" {
			return "n", true
		}
	}
	return "", false
}
