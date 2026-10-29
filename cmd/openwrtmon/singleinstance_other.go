//go:build !windows

package main

func acquireSingleInstance() (any, bool) {
	return nil, true
}

