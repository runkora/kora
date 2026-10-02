//go:build !linux

package main

// hideEnviron is a no-op off Linux, where sandbox-exec only runs for local tests.
func hideEnviron() error { return nil }
