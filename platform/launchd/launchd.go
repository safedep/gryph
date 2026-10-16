// Package launchd writes launchd job definitions and drives launchctl. The
// scheduler and the service installer of macOS share it, so every job of
// Gryph has the same label form and the same property list encoding.
package launchd

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// LabelPrefix starts the label of every job of Gryph.
const LabelPrefix = "io.safedep."

// SystemDomain is the domain of the daemons that launchd starts at boot.
const SystemDomain = "system"

// SocketName is the key of the socket of a job in its Sockets dictionary.
// The job asks launchd for the socket by this name.
const SocketName = "Listeners"

// Label returns the launchd label of the job name.
func Label(name string) string {
	return LabelPrefix + name
}

// GUIDomain returns the domain of the login session of uid.
func GUIDomain(uid int) string {
	return "gui/" + strconv.Itoa(uid)
}

// Entry is one key of a property list dictionary. Value is a string, an
// int, a bool, a []string or a Dict.
type Entry struct {
	Key   string
	Value any
}

// Dict is a property list dictionary. The order of the entries is the
// order of the file, so the same job always gives the same bytes.
type Dict []Entry

// Encode returns the XML property list document of d.
func (d Dict) Encode() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
`)
	if err := writeValue(&b, d, 0); err != nil {
		return nil, err
	}
	b.WriteString("</plist>\n")
	return b.Bytes(), nil
}

func writeValue(b *bytes.Buffer, v any, depth int) error {
	indent := strings.Repeat("\t", depth)
	switch v := v.(type) {
	case string:
		b.WriteString(indent + "<string>")
		if err := xml.EscapeText(b, []byte(v)); err != nil {
			return err
		}
		b.WriteString("</string>\n")
	case int:
		fmt.Fprintf(b, "%s<integer>%d</integer>\n", indent, v)
	case bool:
		fmt.Fprintf(b, "%s<%t/>\n", indent, v)
	case []string:
		b.WriteString(indent + "<array>\n")
		for _, s := range v {
			if err := writeValue(b, s, depth+1); err != nil {
				return err
			}
		}
		b.WriteString(indent + "</array>\n")
	case Dict:
		b.WriteString(indent + "<dict>\n")
		for _, e := range v {
			b.WriteString(indent + "\t<key>")
			if err := xml.EscapeText(b, []byte(e.Key)); err != nil {
				return err
			}
			b.WriteString("</key>\n")
			if err := writeValue(b, e.Value, depth+1); err != nil {
				return err
			}
		}
		b.WriteString(indent + "</dict>\n")
	default:
		return fmt.Errorf("launchd: %T is not a property list value", v)
	}
	return nil
}

// Launchctl runs launchctl through Run, which a test replaces.
type Launchctl struct {
	Run func(ctx context.Context, args ...string) error
}

// Bootstrap loads the job file at path into domain.
func (l Launchctl) Bootstrap(ctx context.Context, domain, path string) error {
	return l.Run(ctx, "bootstrap", domain, path)
}

// Bootout stops the job with label and takes it out of domain.
func (l Launchctl) Bootout(ctx context.Context, domain, label string) error {
	return l.Run(ctx, "bootout", domain+"/"+label)
}

// Loaded reports whether domain holds the job.
func (l Launchctl) Loaded(ctx context.Context, domain, label string) bool {
	return l.Run(ctx, "print", domain+"/"+label) == nil
}
