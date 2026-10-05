package kernel

// BPFLSM is what the probe found about the BPF LSM on this host: whether
// the kernel was built with it, whether it is in the active LSM list, and
// whether the kernel ships its BTF, which a BPF program needs to attach
// to an LSM hook. Each fact is unknown when its source is not readable.
type BPFLSM struct {
	// Built says whether the kernel configuration has CONFIG_BPF_LSM. The
	// empty string means the configuration was not readable.
	Built string
	// ConfigSource names the file the configuration came from.
	ConfigSource string
	// Active is the active LSM list, in order. Nil when securityfs is
	// not mounted.
	Active []string
	// BPFActive is true when bpf is in the active list.
	BPFActive bool
	// BTF is true when the kernel ships its BTF at /sys/kernel/btf/vmlinux.
	BTF bool
}

// Ready reports whether a BPF LSM program could attach on this host: the
// kernel was built with the LSM, the LSM is active, and the BTF is there.
func (b BPFLSM) Ready() bool {
	return b.Built == "y" && b.BPFActive && b.BTF
}

// ProbeBPFLSM runs the probe. Only Linux has the sources; another
// platform returns ErrUnsupported.
func ProbeBPFLSM() (BPFLSM, error) {
	return probeBPFLSM()
}
