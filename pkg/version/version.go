// Package version exposes version information about the TPM device plugin. This includes a version
// number, of course, but potentially any other version specific information as required.
package version

// Version of the TPM Device Plugin. This should be overwritten at compile time with a go linker flag.
var Version string = "dev"
