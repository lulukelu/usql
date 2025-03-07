package env

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/xo/dburl"

	"github.com/xo/usql/text"
)

// Getenv tries retrieving successive keys from os environment variables.
func Getenv(keys ...string) string {
	for _, key := range keys {
		if s := os.Getenv(key); s != "" {
			return s
		}
	}

	return ""
}

// expand expands the tilde (~) in the front of a path to a the supplied
// directory.
func expand(u *user.User, path string) string {
	if path == "~" {
		return u.HomeDir
	} else if strings.HasPrefix(path, "~/") {
		return filepath.Join(u.HomeDir, strings.TrimPrefix(path, "~/"))
	}

	return path
}

// unquote unquotes a string.
func unquote(s string, c rune) (string, error) {
	if len(s) < 2 || rune(s[len(s)-1]) != c {
		return "", text.ErrUnterminatedString
	}

	return s[1 : len(s)-1], nil
}

// Getvar retrieves a variable.
func Getvar(s string) (bool, string, error) {
	q, n := "", s
	if c := rune(s[0]); c == '\'' || c == '"' {
		var err error
		n, err = unquote(s, c)
		if err != nil {
			return false, "", err
		}
		q = string(c)
	}

	if v, ok := vars[n]; ok {
		return true, q + v + q, nil
	}

	return false, s, nil
}

// OpenFile opens a file for reading, returning the full, expanded path of the
// file.  All callers are responsible for closing the returned file.
func OpenFile(u *user.User, path string, relative bool) (string, *os.File, error) {
	var err error

	path, err = filepath.EvalSymlinks(expand(u, path))
	switch {
	case err != nil && os.IsNotExist(err):
		return "", nil, text.ErrNoSuchFileOrDirectory
	case err != nil:
		return "", nil, err
	}

	fi, err := os.Stat(path)
	switch {
	case err != nil && os.IsNotExist(err):
		return "", nil, text.ErrNoSuchFileOrDirectory
	case err != nil:
		return "", nil, err
	case fi.IsDir():
		return "", nil, text.ErrCannotIncludeDirectories
	}

	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return "", nil, err
	}

	return path, f, nil
}

// HistoryFile returns the path to the history file.
//
// Defaults to ~/.<command name>_history, overridden by environment variable
// <COMMAND NAME>_HISTORY (ie, ~/.usql_history and USQL_HISTORY).
func HistoryFile(u *user.User) string {
	n := text.CommandUpper() + "_HISTORY"
	path := "~/." + strings.ToLower(n)
	if s := Getenv(n); s != "" {
		path = s
	}

	return expand(u, path)
}

// RCFile returns the path to the RC file.
//
// Defaults to ~/.<command name>rc, overridden by environment variable
// <COMMAND NAME>RC (ie, ~/.usqlrc and USQLRC).
func RCFile(u *user.User) string {
	n := text.CommandUpper() + "RC"
	path := "~/." + strings.ToLower(n)
	if s := Getenv(n); s != "" {
		path = s
	}

	return expand(u, path)
}

// PassFile returns the path to the password file.
//
// Defaults to ~/.<command name>pass, overridden by environment variable
// <COMMAND NAME>PASS (ie, ~/.usqlpass and USQLPASS).
func PassFile(u *user.User) string {
	n := text.CommandUpper() + "PASS"
	path := "~/." + strings.ToLower(n)
	if s := Getenv(n); s != "" {
		path = s
	}

	return expand(u, path)
}

// PassFileEntry determines if there is a password file entry for a specific
// database URL.
func PassFileEntry(u *user.User, v *dburl.URL) (*url.Userinfo, error) {
	// check if v already has password defined ...
	var username string
	if v.User != nil {
		username = v.User.Username()
		if _, ok := v.User.Password(); ok {
			return nil, nil
		}
	}

	// check if pass file exists
	path := PassFile(u)
	fi, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}

	// check pass file is not directory
	if fi.IsDir() {
		return nil, fmt.Errorf(text.BadPassFile, path)
	}

	// check pass file is not group/world readable/writable/executable
	if runtime.GOOS != "windows" && fi.Mode()&0x3f != 0 {
		return nil, fmt.Errorf(text.BadPassFileMode, path)
	}

	// read pass file entries
	entries, err := readPassEntries(path)
	if err != nil {
		return nil, err
	}

	// find matching entry
	n := strings.Split(v.Normalize(":", "", 3), ":")
	if len(n) < 3 {
		return nil, errors.New("unknown error encountered normalizing URL")
	}
	for _, entry := range entries {
		if u, p, ok := matchPassEntry(n, entry); ok {
			if u == "*" {
				u = username
			}
			return url.UserPassword(u, p), nil
		}
	}

	return nil, nil
}

var commentRE = regexp.MustCompile(`#.*`)

// readPassEntries reads the pass file entries from path.
func readPassEntries(path string) ([][]string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries [][]string
	s := bufio.NewScanner(f)
	i := 0
	for s.Scan() {
		i++

		// grab next line
		line := strings.TrimSpace(commentRE.ReplaceAllString(s.Text(), ""))
		if line == "" {
			continue
		}

		// split and check length
		v := strings.Split(line, ":")
		if len(v) != 6 {
			return nil, fmt.Errorf(text.BadPassFileLine, i)
		}

		// make sure no blank entries exist
		for j := 0; j < len(v); j++ {
			if v[j] == "" {
				return nil, fmt.Errorf(text.BadPassFileFieldEmpty, i, j)
			}
		}

		entries = append(entries, v)
	}

	return entries, nil
}

// matchPassEntry takes a normalized n, and a password entry along with the
// read username and pass, and determines if all of the components in n match entry.
func matchPassEntry(n, entry []string) (string, string, bool) {
	for i := 0; i < len(n); i++ {
		if entry[i] != "*" && entry[i] != n[i] {
			return "", "", false
		}
	}

	return entry[4], entry[5], true
}

// Unquote unquotes the string.
func Unquote(u *user.User, s string) (string, error) {
	if s == "" {
		return "", nil
	}

	if len(s) > 1 {
		c := rune(s[0])
		switch {
		case c == ':':
			ok, v, err := Getvar(s[1:])
			if err != nil {
				return "", err
			}

			if ok {
				return v, nil
			}

			return s, nil

		case c == '\'' || c == '"':
			return unquote(s, c)
		}
	}

	return s, nil
}
