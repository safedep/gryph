//go:build !linux

package kernel

func probeBPFLSM() (BPFLSM, error) { return BPFLSM{}, ErrUnsupported }
