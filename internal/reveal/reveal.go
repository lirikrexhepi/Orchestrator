// Package reveal opens a path in the operating system's file manager.
package reveal

import (
	"errors"
	"os"
)

// Path shows a file or directory in the desktop file manager.
//
// A file is selected inside its folder where the platform supports it, so the
// user lands on the thing they asked about rather than on a directory listing.
func Path(target string) error {
	if target == "" {
		return errors.New("no path given")
	}
	if _, err := os.Stat(target); err != nil {
		return err
	}
	return open(target)
}

func File(target string) error {
	if target == "" {
		return errors.New("no path given")
	}
	if _, err := os.Stat(target); err != nil {
		return err
	}
	return launch(target)
}
