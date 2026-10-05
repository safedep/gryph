package kernel

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeBPFLSM(t *testing.T) {
	dir := t.TempDir()
	list := filepath.Join(dir, "lsm")
	btf := filepath.Join(dir, "vmlinux")
	config := filepath.Join(dir, "config.gz")
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, err := w.Write([]byte("CONFIG_LSM=\"landlock,bpf\"\nCONFIG_BPF_LSM=y\n# CONFIG_OTHER is not set\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, os.WriteFile(config, gz.Bytes(), 0o644))

	lsmListPath, btfPath, configPaths = list, btf, func() []string { return []string{filepath.Join(dir, "missing"), config} }

	t.Run("every source missing", func(t *testing.T) {
		b, err := ProbeBPFLSM()
		require.NoError(t, err)
		assert.Nil(t, b.Active)
		assert.False(t, b.BTF)
		assert.Equal(t, "y", b.Built)
		assert.Equal(t, config, b.ConfigSource)
		assert.False(t, b.Ready())
	})

	t.Run("active list without bpf", func(t *testing.T) {
		require.NoError(t, os.WriteFile(list, []byte("landlock,yama,apparmor\n"), 0o644))
		b, err := ProbeBPFLSM()
		require.NoError(t, err)
		assert.Equal(t, []string{"landlock", "yama", "apparmor"}, b.Active)
		assert.False(t, b.BPFActive)
		assert.False(t, b.Ready())
	})

	t.Run("ready", func(t *testing.T) {
		require.NoError(t, os.WriteFile(list, []byte("landlock,yama,apparmor,bpf\n"), 0o644))
		require.NoError(t, os.WriteFile(btf, []byte{}, 0o644))
		b, err := ProbeBPFLSM()
		require.NoError(t, err)
		assert.True(t, b.BPFActive)
		assert.True(t, b.BTF)
		assert.True(t, b.Ready())
	})

	t.Run("not set in a plain config", func(t *testing.T) {
		plain := filepath.Join(dir, "config-plain")
		require.NoError(t, os.WriteFile(plain, []byte("# CONFIG_BPF_LSM is not set\n"), 0o644))
		value, ok := configValue(plain, "CONFIG_BPF_LSM")
		assert.True(t, ok)
		assert.Equal(t, "n", value)
	})
}
