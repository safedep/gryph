package privacy

import "regexp"

// urlPattern finds an absolute URL in text: a scheme, an optional userinfo,
// a host with an optional port, an optional path, and an optional query or
// fragment. The groups keep what the export may show.
var urlPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)(?:[^\s/?#@]*@)?([^\s/?#]+)([^\s?#]*)(?:[?#][^\s]*)?`)

// StripURLs removes the userinfo, the query and the fragment of every URL
// in s, and keeps the scheme, the host and the path. A query often carries
// a token, and the userinfo a password, while the host and the path are
// what a rule matches on.
func StripURLs(s string) string {
	if s == "" {
		return s
	}
	return urlPattern.ReplaceAllString(s, "$1$2$3")
}
