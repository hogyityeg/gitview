# gitview

- Commit and push finished, verified changes straight to `main`, without asking. Run `gofmt -l .`, `go vet ./...` and `go test ./...` first.
- After any change, rebuild the installed command with `go build -o ~/.local/bin/gitview .`. Never report a change as done until the `gitview` command is updated.
