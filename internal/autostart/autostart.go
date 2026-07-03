// Package autostart enables/disables launching dropzone at user login, without
// admin rights, using the native per-OS mechanism.
package autostart

// Set enables or disables autostart and returns the resulting state.
func Set(enabled bool) (bool, error) {
	if enabled {
		if err := Enable(); err != nil {
			return IsEnabled(), err
		}
		return true, nil
	}
	if err := Disable(); err != nil {
		return IsEnabled(), err
	}
	return false, nil
}
