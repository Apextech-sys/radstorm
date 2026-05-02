// Command radstorm-api — version metadata.
//
// Purpose:
//
//	Declares the Version variable for the radstorm-api binary.
//	The value is overwritten at release build time via -ldflags:
//	  -X main.Version=$(git describe --tags --always)
//	During development and test builds the default "dev" is used.
//
// Related files:
//   - apps/api/cmd/radstorm-api/main.go (uses Version in startup log)
//   - .github/workflows/release.yml (sets the ldflags)
//
// Briefing: .orchestration/briefings/wave-6-release-pipeline.md
//
// Contract: internal — consumed by release build only.
package main

// Version is overridden via -ldflags in release builds.
// Default "dev" is used for local and CI non-release builds.
var Version = "dev"
