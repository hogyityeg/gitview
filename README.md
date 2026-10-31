# gitview

A live, read-only `gitk --all` in the browser, for one repository or many.

Run `gitview` in a folder and it finds the git repositories under it and shows
their commit graphs side by side, refreshing every few seconds. It's handy for
watching branches move while you (or your coding agents) work, especially on a
remote machine or across a polyrepo setup.

- Every branch, tag and remote, drawn as a graph with lanes, like `gitk --all`
- Several repositories at once, in a grid you can toggle per repo
- Updates on its own, and pauses while the tab is hidden
- Marks the checked-out branch and branches checked out in linked worktrees
- Click a commit's short hash to copy the full hash
- One static binary with the web page embedded; it only reads from git

## Install

Requires Go 1.27+ and `git` on the `PATH`.

```sh
go install github.com/hogyityeg/gitview@latest
```

This puts `gitview` in `$(go env GOPATH)/bin`. From a checkout,
`go build -o ~/.local/bin/gitview .` does the same.

## Usage

```sh
cd ~/projects/my-app && gitview      # this repo (or the repos under it)
gitview ~/projects/platform          # or name the folder
```

It prints the URLs it listens on; open one in a browser and press Ctrl-C to
stop it. It isn't a service: start it where you want to see git.

Repositories are found up to `-depth` folders deep. It doesn't look inside a
repository once it finds one, and it skips hidden folders and `node_modules`,
`vendor`, `venv`, `__pycache__`, `dist`, `build` and `target`.

| Flag        | Default | Meaning                                    |
| ----------- | ------- | ------------------------------------------ |
| `-addr`     |         | listen on this address instead (see below) |
| `-n`        | 400     | commits shown per repository               |
| `-depth`    | 5       | how deep to look for repositories          |
| `-interval` | 5s      | how often the browser refreshes            |

### Choosing repositories

A folder starts with all its repositories shown. Click one in the sidebar to
hide or show it. The choice is saved per folder in
`~/.config/gitview/config.json`, so the next `gitview` in that folder shows the
same ones.

### Network

By default gitview listens on `127.0.0.1`, plus the machine's Tailscale IPv4
when [Tailscale](https://tailscale.com) is running. That makes it reachable
from your other devices on the tailnet, at `http://<machine>.<tailnet>.ts.net:<port>`
(printed on start), but not from the local network. The port is the first
free one from 7420, so several folders can be open at once.

Use `-addr` to pick the address yourself, e.g. `-addr 127.0.0.1:8000`. There
is no authentication, so don't expose it to a network you don't trust.

## Development

```sh
go test ./...        # unit tests
go build .           # the web page in web/ is embedded, so rebuild after changing it
```

## License

[MIT](LICENSE)
