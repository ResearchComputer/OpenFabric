package main

import (
	"opentela/entry/cmd"
	"opentela/internal/common"
)

var (
	// Populated during build via ldflags
	version   = "dev"
	commitHash = "?"
	buildDate  = ""
	// buildSig is the hex-encoded Ed25519 signature over "version|commitHash",
	// injected by the release pipeline using the maintainer's private key.
	buildSig string
)

func main() {
	common.JSONVersion.Version = version
	common.JSONVersion.Commit = commitHash
	common.JSONVersion.Date = buildDate
	common.JSONVersion.BuildSig = buildSig
	cmd.Execute()
}
