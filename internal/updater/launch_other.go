//go:build !windows

package updater

// LaunchInstaller is not implemented for non-Windows targets yet.
func LaunchInstaller(installerPath, installDir string) error {
	return ErrUnsupportedOS
}
