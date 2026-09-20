package epub

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

type EPUBTarget struct {
	Href     string
	Fragment string
}

var schemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*:`)

// resolveTarget resolves a URI reference against baseDocument, which is a document
// name, not a directory.
func resolveTarget(baseDocument, reference string) (EPUBTarget, error) {
	ref := strings.TrimSpace(reference)
	if ref == "" {
		return EPUBTarget{}, fmt.Errorf("empty reference")
	}

	rawPath, fragment, _ := strings.Cut(ref, "#")
	rawPath, _, _ = strings.Cut(rawPath, "?")

	if strings.HasPrefix(rawPath, "//") || schemePattern.MatchString(rawPath) {
		return EPUBTarget{}, fmt.Errorf("external reference: %s", reference)
	}
	lower := strings.ToLower(rawPath)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return EPUBTarget{}, fmt.Errorf("encoded separator: %s", reference)
	}

	href := baseDocument
	if decoded := decodeOnce(rawPath); decoded != "" {
		if err := checkFileReference(decoded); err != nil {
			return EPUBTarget{}, err
		}
		resolved, err := cleanArchivePath(path.Join(path.Dir(baseDocument), decoded))
		if err != nil {
			return EPUBTarget{}, err
		}
		href = resolved
	}
	if href == "" {
		return EPUBTarget{}, fmt.Errorf("unresolvable reference: %s", reference)
	}

	return EPUBTarget{Href: href, Fragment: decodeOnce(fragment)}, nil
}

// NormalizeArchivePath validates a canonical ZIP entry name. It performs no
// percent-decoding: '%', '#' and '?' are ordinary characters in an entry name.
func NormalizeArchivePath(entry string) (string, error) {
	return cleanArchivePath(entry)
}

func cleanArchivePath(name string) (string, error) {
	if err := checkFileReference(name); err != nil {
		return "", err
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path escapes the archive root: %s", name)
	}
	return clean, nil
}

func checkFileReference(name string) error {
	if name == "" {
		return fmt.Errorf("empty path")
	}
	if strings.ContainsAny(name, "\\\x00") {
		return fmt.Errorf("invalid path: %s", name)
	}
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("absolute path: %s", name)
	}
	if last := name[strings.LastIndex(name, "/")+1:]; last == "" || last == "." || last == ".." {
		return fmt.Errorf("directory path: %s", name)
	}
	return nil
}

func decodeOnce(s string) string {
	if decoded, err := url.PathUnescape(s); err == nil {
		return decoded
	}
	return s
}
