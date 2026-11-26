# gitview

- Commit and push finished, verified changes straight to `main`, without asking. Run `gofmt -l .`, `go vet ./...` and `go test ./...` first.
- Release every pushed change as a semver tag on that commit (patch for fixes, minor for features, see README "Releases"): `git tag vX.Y.Z && git push origin vX.Y.Z`, then rebuild, so the installed command reports a clean version.
- After any change, rebuild the installed command with `go build -o ~/.local/bin/gitview .`. Never report a change as done until the `gitview` command is updated.
