// Package buildinfo holds the version the binary was built as. Release builds set it with
//
//	-ldflags "-X github.com/njiwa-tz/mail/internal/buildinfo.Version=v1.2.3"
//
// A build without it reports "dev".
package buildinfo

// Version is the release this binary was built from.
var Version = "dev"
