package cli

import (
	"github.com/vessica-labs/vessica-cli/internal/harness"
	"github.com/vessica-labs/vessica-cli/internal/onboarding"
)

func startingHarnessFindings(profile onboarding.RepositoryProfile) harness.RepositoryFindings {
	return harness.RepositoryFindings{
		Name: profile.Name, Remote: profile.Remote, Stack: profile.Stack,
		Languages: profile.Languages, Frameworks: profile.Frameworks, PackageManagers: profile.PackageManagers,
		Manifests: profile.Manifests, Dependencies: profile.Dependencies, Components: profile.Components,
		EntryPoints: profile.EntryPoints, Directories: profile.Directories, CI: profile.CI, Deploy: profile.Deploy,
		Risks: profile.Risks, Unresolved: profile.Unresolved, Commands: profile.Commands,
	}
}

func missingHarnessTargets(profile onboarding.RepositoryProfile) []string {
	var out []string
	for _, name := range []string{"AGENTS.md", "ARCHITECTURE.md", "DESIGN.md", "DEPLOY.md", "TESTING.md", "SECURITY.md", ".vessica/harness.yaml"} {
		if profile.Fingerprint[name] == "absent" {
			out = append(out, name)
		}
	}
	return out
}
