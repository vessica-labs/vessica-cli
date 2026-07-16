// Package toolchain centralizes immutable versions installed into hosted worker
// sandboxes. Updating a version must invalidate the Railway checkpoint name.
package toolchain

const (
	CodexVersion      = "0.144.1"
	PlaywrightVersion = "1.61.1"
	PNPMVersion       = "11.9.0"
	CheckpointVersion = "5"
)
