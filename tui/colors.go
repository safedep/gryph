package tui

// ANSI color codes
const (
	Reset = "\033[0m"
	Bold  = "\033[1m"
	Dim   = "\033[2m"

	// Foreground colors
	Black   = "\033[30m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
	Gray    = "\033[90m"

	// Bright foreground colors
	BrightRed     = "\033[91m"
	BrightGreen   = "\033[92m"
	BrightYellow  = "\033[93m"
	BrightBlue    = "\033[94m"
	BrightMagenta = "\033[95m"
	BrightCyan    = "\033[96m"
	BrightWhite   = "\033[97m"

	// Bold variants
	BoldWhite = "\033[1;37m"
)

// Colorizer wraps text with ANSI color codes if colors are enabled.
type Colorizer struct {
	enabled bool
}

// NewColorizer creates a new Colorizer.
func NewColorizer(enabled bool) *Colorizer {
	return &Colorizer{enabled: enabled}
}

// Apply applies the given color to the text.
func (c *Colorizer) Apply(color, text string) string {
	if !c.enabled {
		return text
	}
	return color + text + Reset
}

// Header formats text as a header.
func (c *Colorizer) Header(text string) string {
	return c.Apply(BoldWhite, text)
}

// Agent formats an agent name.
func (c *Colorizer) Agent(text string) string {
	return c.Apply(Cyan, text)
}

// Path formats a file path.
func (c *Colorizer) Path(text string) string {
	return c.Apply(Blue, text)
}

// Success formats success text.
func (c *Colorizer) Success(text string) string {
	return c.Apply(Green, text)
}

// Error formats error text.
func (c *Colorizer) Error(text string) string {
	return c.Apply(Red, text)
}

// Warning formats warning text.
func (c *Colorizer) Warning(text string) string {
	return c.Apply(Yellow, text)
}

// Dim formats secondary/dim text.
func (c *Colorizer) Dim(text string) string {
	return c.Apply(Gray, text)
}

// Cyan formats text in cyan color.
func (c *Colorizer) Cyan(text string) string {
	return c.Apply(Cyan, text)
}

// Number formats numbers/stats.
func (c *Colorizer) Number(text string) string {
	return c.Apply(Yellow, text)
}

// StatusOK formats an OK status indicator.
func (c *Colorizer) StatusOK() string {
	return c.Apply(Green, "[ok]")
}

// StatusFail formats a fail status indicator.
func (c *Colorizer) StatusFail() string {
	return c.Apply(Red, "[!!]")
}

// StatusSkip formats a skip status indicator.
func (c *Colorizer) StatusSkip() string {
	return c.Apply(Gray, "[--]")
}

// DiffAdd formats added lines in diff.
func (c *Colorizer) DiffAdd(text string) string {
	return c.Apply(Green, text)
}

// DiffRemove formats removed lines in diff.
func (c *Colorizer) DiffRemove(text string) string {
	return c.Apply(Red, text)
}

// DiffHeader formats diff headers.
func (c *Colorizer) DiffHeader(text string) string {
	return c.Apply(Cyan, text)
}
