//go:build !windows

package autostart

func IsEnabled() bool       { return false }
func Set(enable bool) error { return nil }
func Toggle() (bool, error) { return false, nil }

