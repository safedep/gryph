package shellcmd

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{`C:\x\audit.db.`, "C:/x/audit.db"},
		{`C:\x\gryph. \audit.db`, "C:/x/gryph/audit.db"},
		{`C:\x\..\audit.db`, "C:/x/../audit.db"},
		{`C:\x\audit.db::$DATA`, "C:/x/audit.db"},
		{`C:\x\audit.db:stream`, "C:/x/audit.db"},
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
		{`$Env:APPDATA\x`, 0},
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

func TestWindowsWords(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{`del C:\x\gr"y"ph\a.db`, []string{"del", `C:\x\gryph\a.db`}},
		{`del "C:\Program Files\x"`, []string{"del", `C:\Program Files\x`}},
		{`del C:\x\gry^ph\a.db`, []string{"del", `C:\x\gryph\a.db`}},
		{"Remove-Item C:\\x\\s`afedep", []string{"Remove-Item", `C:\x\safedep`}},
		{`a;b|c&d (e),f`, []string{"a", "b", "c", "d", "e", "f"}},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			assert.Equal(t, tc.want, windowsWords(tc.line))
		})
	}
}

func TestAnalyze_WindowsForms(t *testing.T) {
	const data = "C:/Users/a/AppData/Local/safedep/gryph"
	const db = data + "/audit.db"
	read := Target{Path: db, Access: AccessRead, Guess: true, Shallow: true}
	cases := []struct {
		name    string
		command string
		want    Target
	}{
		{"git bash quoted backslash path", `rm 'C:\Users\a\AppData\Local\safedep\gryph\audit.db'`, Target{Path: db, Access: AccessRemove}},
		{"git bash drive form", "rm /c/Users/a/AppData/Local/safedep/gryph/audit.db", Target{Path: db, Access: AccessRemove}},
		{"git bash variable", "rm $LOCALAPPDATA/safedep/gryph/audit.db", Target{Path: db, Access: AccessRemove}},
		{"git bash braced variable", "rm ${USERPROFILE}/AppData/Local/safedep/gryph/audit.db", Target{Path: db, Access: AccessRemove}},
		{"powershell remove", `Remove-Item $env:LOCALAPPDATA\safedep\gryph\audit.db`, Target{Path: db, Access: AccessRemove}},
		{"powershell variable in other case", `ri $ENV:localappdata\safedep\gryph\audit.db`, Target{Path: db, Access: AccessRemove}},
		{"powershell docs casing", `Remove-Item $Env:LOCALAPPDATA\safedep\gryph\audit.db`, Target{Path: db, Access: AccessRemove}},
		{"powershell write", `Set-Content $env:LOCALAPPDATA\safedep\gryph\audit.db x`, Target{Path: db, Access: AccessWriteTree}},
		{"powershell braced variable", `Get-Content ${env:LOCALAPPDATA}\safedep\gryph\audit.db`, read},
		{"powershell parameter value", `Remove-Item -Path:$env:LOCALAPPDATA\safedep -Recurse`, Target{Path: "C:/Users/a/AppData/Local/safedep", Access: AccessRemove}},
		{"powershell home", `Remove-Item $HOME\AppData\Local\safedep\gryph\audit.db`, Target{Path: db, Access: AccessRemove}},
		{"powershell join-path", `Get-Content (Join-Path $env:LOCALAPPDATA 'safedep\gryph\audit.db')`, read},
		{"powershell join-path parameters", `Get-Content (Join-Path -Path $env:LOCALAPPDATA -ChildPath safedep\gryph\audit.db)`, read},
		{"powershell plus", `Get-Content ($env:LOCALAPPDATA + '\safedep\gryph\audit.db')`, read},
		{"powershell empty variable", `Remove-Item "${null}C:\Users\a\AppData\Local\safedep\gryph\audit.db"`, Target{Path: db, Access: AccessRemove}},
		{"powershell unknown variable in a name", `Remove-Item "$env:LOCALAPPDATA\safedep\gryph\audit$z.db"`, Target{Path: db, Access: AccessRemove}},
		{"powershell unknown variable as a glob", `Remove-Item $env:LOCALAPPDATA\safedep\$x\audit.db`, Target{Path: "C:/Users/a/AppData/Local/safedep", Glob: "C:/Users/a/AppData/Local/safedep/*/audit.db", Access: AccessRemove}},
		{"quote inside a word", `Remove-Item C:\Users\a\AppData\Local\safedep\gr"y"ph\audit.db`, Target{Path: db, Access: AccessRemove}},
		{"cmd delete", `del %LOCALAPPDATA%\safedep\gryph\audit.db`, Target{Path: db, Access: AccessRemove}},
		{"cmd caret", `del C:\Users\a\AppData\Local\safedep\gry^ph\audit.db`, Target{Path: db, Access: AccessRemove}},
		{"cmd substring", `del %LOCALAPPDATA:~0%\safedep\gryph\audit.db`, Target{Path: db, Access: AccessRemove}},
		{"cmd stream", `cmd /c "echo x> %LOCALAPPDATA%\safedep\gryph\audit.db::$DATA"`, Target{Path: db, Access: AccessWriteTree}},
		{"trailing dot", `Remove-Item C:\Users\a\AppData\Local\safedep\gryph.\audit.db.`, Target{Path: db, Access: AccessRemove}},
		{"device prefix", `type \\?\C:\Users\a\AppData\Local\safedep\gryph\audit.db`, read},
		{"dot segment", `del C:\Users\a\AppData\Local\safedep\x\..\gryph\audit.db`, Target{Path: db, Access: AccessRemove}},
		{"option value", `tool --file=C:\Users\a\AppData\Local\safedep\gryph\audit.db`, Target{Path: db, Access: AccessWriteTree}},
		{"glob", `Remove-Item C:\Users\a\AppData\Local\safedep\gryph\*`, Target{Path: data, Glob: data + "/*", Access: AccessRemove}},
		{"read and delete of one path", "cat " + db + "; del " + db, Target{Path: db, Access: AccessRemove}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Contains(t, Analyze(tc.command, windowsTestEnv).Targets, tc.want)
		})
	}
}

func TestScanWindows_Skips(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{"home form is the parser's", "rm ~/x"},
		{"home variable is the parser's", "rm $HOME/x"},
		{"url", "curl https://x.example/a"},
		{"relative path", `del safedep\gryph\audit.db`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, scanWindows(tc.command, windowsTestEnv).targets)
		})
	}
}

func TestAnalyze_WindowsScanKeepsTheParsersAccess(t *testing.T) {
	for _, command := range []string{"cat C:/work/proj/.claude/settings.json", "cat C:/work/proj/.claude/*.json"} {
		t.Run(command, func(t *testing.T) {
			for _, target := range Analyze(command, windowsTestEnv).Targets {
				assert.NotEqual(t, AccessWriteTree, target.Access, "a read of a known tool is not a change: %+v", target)
			}
		})
	}
}

func TestAnalyze_NoWindowsScanOutsideWindows(t *testing.T) {
	env := windowsTestEnv
	env.Windows = false
	for _, target := range Analyze(`del %LOCALAPPDATA%\safedep\gryph\audit.db`, env).Targets {
		assert.False(t, strings.HasPrefix(target.Path, "C:/Users"), "%+v", target)
	}
}

func TestAnalyze_WindowsGryphCalls(t *testing.T) {
	cases := []struct {
		name    string
		command string
		hook    bool
		resolve bool
	}{
		{"cmd wrapper", "cmd /c gryph _hook claude-code PreToolUse", true, false},
		{"powershell wrapper", `powershell -c "& 'C:\Program Files\Gryph\GRYPH.EXE' _hook x y"`, true, false},
		{"backslash program path", `'C:\Program Files\Gryph\gryph.exe' _hook claude-code PreToolUse`, true, false},
		{"cmd keep wrapper", `cmd.exe /K "cd x & gryph _hook x y"`, true, false},
		{"powershell short flag", `powershell.exe -co "gryph _hook x y"`, true, false},
		{"powershell resolve", `pwsh -Command "gryph policy approve resolve --id x"`, false, true},
		{"other program", `cmd /c echo hello`, false, false},
		{"hook words in an argument", `findstr "gryph _hook" notes.txt`, false, false},
		{"hook words in a commit message", `git commit -m "fix gryph _hook"`, false, false},
		{"powershell with another flag", `powershell -NoProfile -File x.ps1 gryph _hook`, false, false},
		{"cmd flags before the command flag", `cmd.exe /d /s /c "gryph _hook x y"`, true, false},
		{"powershell flags before the command flag", `powershell -NoProfile -ExecutionPolicy Bypass -Command "gryph _hook x y"`, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := Analyze(tc.command, windowsTestEnv)
			assert.Equal(t, tc.hook, a.GryphHook)
			assert.Equal(t, tc.resolve, a.GryphResolve)
		})
	}
}

func TestAnalyze_WindowsScanOfALongLine(t *testing.T) {
	var b strings.Builder
	b.WriteString("git add")
	for i := range 20000 {
		b.WriteString(` C:\work\src\f`)
		b.WriteString(strings.Repeat("x", i%7))
	}
	start := time.Now()
	a := Analyze(b.String(), windowsTestEnv)
	require.Less(t, time.Since(start), fuzzTimeBound, "the scan grows linearly")
	assert.True(t, a.Parsed)
	assert.Less(t, len(a.Targets), 100, "the scan keeps one target per path")
}
