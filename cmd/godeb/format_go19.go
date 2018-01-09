// +build go1.9,!go1.10

package main

func init() {
	// This will break at build time, and print out the error.
	"OOPS! Go 1.9 generates tar files that cannot be processed by deb. Please build with Go <=1.8 or >=1.10."

	// Just in case.
	panic("nope, how come it built!?")
}
