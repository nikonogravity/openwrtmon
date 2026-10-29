//go:build windows

package autostart

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const runKeyName = `OpenWRTMon`

func IsEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	val, _, err := k.GetStringValue(runKeyName)
	return err == nil && val != ""
}

func Set(enable bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if enable {
		exePath, err := os.Executable()
		if err != nil {
			return err
		}
		val := fmt.Sprintf(`"%s"`, exePath)
		return k.SetStringValue(runKeyName, val)
	}

	err = k.DeleteValue(runKeyName)
	if err == registry.ErrNotExist {
		return nil
	}
	return err
}

func Toggle() (bool, error) {
	current := IsEnabled()
	target := !current
	if err := Set(target); err != nil {
		return current, err
	}
	return target, nil
}

