package shellcmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var windowsTestEnv = Env{
	WorkingDir: "C:/work",
	Home:       "C:/Users/a",
	Windows:    true,
	Vars: map[string]string{
		"USERPROFILE":  "C:/Users/a",
		"APPDATA":      "C:/Users/a/AppData/Roaming",
		"LOCALAPPDATA": "C:/Users/a/AppData/Local",
	},
}

func TestEnv_Native(t *testing.T) {
	cases := []struct {
		in      string
		windows string
	}{
		{`C:\Users\a\x`, "C:/Users/a/x"},
		{"/c/Users/a/x", "C:/Users/a/x"},
		{"/c", "C:/"},
		{"/C:/Users/a/x", "C:/Users/a/x"},
		{`\\?\C:\Users\a\x`, "C:/Users/a/x"},
		{`\\.\C:\x`, "C:/x"},
		{"/tmp/x", "/tmp/x"},
		{"/cc/x", "/cc/x"},
		{"x/y", "x/y"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.windows, windowsTestEnv.native(tc.in))
			assert.Equal(t, tc.in, Env{}.native(tc.in), "outside Windows a path stays as it is")
		})
	}
}

func TestWindowsPathStart(t *testing.T) {
	cases := []struct {
		word string
		want int
	}{
		{`C:\x`, 0},
		{"C:/x", 0},
		{`--file=C:\x`, 7},
		{`-fC:\x`, 2},
		{`-Path:%APPDATA%\x`, 6},
		{`-Path:$env:APPDATA\x`, 6},
		{`${env:APPDATA}\x`, 0},
		{`$HOME\x`, 0},
		{`\\?\C:\x`, 0},
		{"file:///C:/x", 8},
		{"https://x.example", -1},
		{"~/x", -1},
		{"$HOME/x", -1},
		{"plain", -1},
	}
	for _, tc := range cases {
		t.Run(tc.word, func(t *testing.T) {
			assert.Equal(t, tc.want, windowsPathStart(tc.word))
		})
	}
}

func TestAnalyze_WindowsForms(t *testing.T) {
	const db = "C:/Users/a/AppData/Local/safedep/gryph/audit.db"
	cases := []struct {
		name    string
		command string
		want    Target
	}{
		{"git bash quoted backslash path", `rm 'C:\Users\a\AppData\Local\safedep\gryph\audit.db'`, Target{Path: db, Access: AccessRemove}},
		{"git bash drive form", "rm /c/Users/a/AppData/Local/safedep/gryph/audit.db", Target{Path: db, Access: AccessRemove}},
		{"git bash variable", "rm $LOCALAPPDATA/safedep/gryph/audit.db", Target{Path: db, Access: AccessRemove}},
		{"git bash braced variable", "rm ${USERPROFILE}/AppData/Local/safedep/gryph/audit.db", Target{Path: db, Access: AccessRemove}},
		{"powershell variable", `Remove-Item $env:LOCALAPPDATA\safedep\gryph\audit.db`, Target{Path: db, Access: AccessWriteTree}},
		{"powershell variable in other case", `Remove-Item $env:localappdata\safedep\gryph\audit.db`, Target{Path: db, Access: AccessWriteTree}},
		{"powershell braced variable", `Get-Content ${env:LOCALAPPDATA}\safedep\gryph\audit.db`, Target{Path: db, Access: AccessRead, Guess: true, Shallow: true}},
		{"powershell parameter value", `Remove-Item -Path:$env:LOCALAPPDATA\safedep\gryph -Recurse`, Target{Path: "C:/Users/a/AppData/Local/safedep/gryph", Access: AccessWriteTree}},
		{"powershell home", `Remove-Item $HOME\AppData\Local\safedep\gryph\audit.db`, Target{Path: db, Access: AccessWriteTree}},
		{"cmd variable", `del %LOCALAPPDATA%\safedep\gryph\audit.db`, Target{Path: db, Access: AccessWriteTree}},
		{"device prefix", `type \\?\C:\Users\a\AppData\Local\safedep\gryph\audit.db`, Target{Path: db, Access: AccessRead, Guess: true, Shallow: true}},
		{"dot segment", `del C:\Users\a\AppData\Local\safedep\x\..\gryph\audit.db`, Target{Path: db, Access: AccessWriteTree}},
		{"option value", `tool --file=C:\Users\a\AppData\Local\safedep\gryph\audit.db`, Target{Path: db, Access: AccessWriteTree}},
		{"glob", `Remove-Item C:\Users\a\AppData\Local\safedep\gryph\*`, Target{Path: "C:/Users/a/AppData/Local/safedep/gryph", Glob: "C:/Users/a/AppData/Local/safedep/gryph/*", Access: AccessWriteTree}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Contains(t, Analyze(tc.command, windowsTestEnv).Targets, tc.want)
		})
	}
}

func TestAnalyze_WindowsScanSkips(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{"posix form is the parser's", "cd ~/.cc && tar -xOf a.tar"},
		{"unknown variable", `Remove-Item $env:OTHER\safedep\gryph\audit.db`},
		{"url", "curl https://x.example/a"},
		{"relative path", `del safedep\gryph\audit.db`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, target := range Analyze(tc.command, windowsTestEnv).Targets {
				assert.NotEqual(t, AccessWriteTree, target.Access, "%+v", target)
			}
		})
	}
}

func TestAnalyze_NoWindowsScanOutsideWindows(t *testing.T) {
	env := windowsTestEnv
	env.Windows = false
	a := Analyze(`del %LOCALAPPDATA%\safedep\gryph\audit.db`, env)
	for _, target := range a.Targets {
		assert.NotEqual(t, AccessWriteTree, target.Access, "%+v", target)
	}
}
