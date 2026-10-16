package launchd

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDict_Encode(t *testing.T) {
	data, err := Dict{
		{"Label", Label("gryph-x")},
		{"ProgramArguments", []string{"/opt/a b/gryph", "<run>"}},
		{"KeepAlive", true},
		{"StartInterval", 900},
		{"Sockets", Dict{{"Listeners", Dict{{"SockPathName", "/var/run/x.sock"}}}}},
	}.Encode()
	require.NoError(t, err)
	assert.Equal(t, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>io.safedep.gryph-x</string>
	<key>ProgramArguments</key>
	<array>
		<string>/opt/a b/gryph</string>
		<string>&lt;run&gt;</string>
	</array>
	<key>KeepAlive</key>
	<true/>
	<key>StartInterval</key>
	<integer>900</integer>
	<key>Sockets</key>
	<dict>
		<key>Listeners</key>
		<dict>
			<key>SockPathName</key>
			<string>/var/run/x.sock</string>
		</dict>
	</dict>
</dict>
</plist>
`, string(data))
}

func TestDict_EncodeRefusesUnknownValue(t *testing.T) {
	_, err := Dict{{"Bad", 1.5}}.Encode()
	assert.ErrorContains(t, err, "float64")
}

func TestLaunchctl(t *testing.T) {
	var got [][]string
	l := Launchctl{Run: func(_ context.Context, args ...string) error {
		got = append(got, args)
		return nil
	}}
	ctx := context.Background()
	require.NoError(t, l.Bootstrap(ctx, SystemDomain, "/Library/LaunchDaemons/x.plist"))
	require.NoError(t, l.Bootout(ctx, GUIDomain(501), "io.safedep.x"))
	assert.True(t, l.Loaded(ctx, SystemDomain, "io.safedep.x"))
	assert.Equal(t, [][]string{
		{"bootstrap", "system", "/Library/LaunchDaemons/x.plist"},
		{"bootout", "gui/501/io.safedep.x"},
		{"print", "system/io.safedep.x"},
	}, got)
}
