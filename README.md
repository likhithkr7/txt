# txt

A local, browser-based editor for `.txt` and `.md` files. One binary, works
offline, and only listens on `127.0.0.1`.

## Features

- **Tabs**: drag to reorder; starts with an `untitled.txt` ready to type in.
- **Markdown**: `.md` files open rendered; **Edit** shows text and live preview side by side.
- **Session restore**: tabs and unsaved text survive reloads and restarts.
- **Safe saves**: atomic writes; never overwrites a file changed on disk.
- **Light and dark themes**, following your system by default.
- **Sandboxed**: can't touch anything outside the folder you open.

## Install

**macOS / Linux**

```sh
curl -fsSL https://raw.githubusercontent.com/likhithkr7/txt/main/install.sh | sh
source ~/.zshrc   # or whatever file the installer names
```

The installer downloads the right build for your machine, verifies its
checksum, puts it in `~/.local/bin`, and adds that folder to your `PATH`.
Pin a version with `VERSION=0.1.0`, or choose a folder with `INSTALL_DIR=~/bin`
(put either before `sh`).

**Windows**: download `txt-<version>-windows-amd64.exe` from
[Releases](https://github.com/likhithkr7/txt/releases), rename it to `txt.exe`,
and put it on your `PATH`.

**From source** (Go 1.26+): `go build -o txt ./cmd/txt`

**Update**: `txt -update`. txt tells you when a new version is out.
**Uninstall**: `rm "$(command -v txt)"`

## Usage

```sh
txt                   # current folder
txt ~/notes           # any folder
txt ~/notes/todo.md   # a folder, opening one file
```

| Flag       | Description                                       |
|------------|---------------------------------------------------|
| `-port N`  | Port to use (default `7777`)                      |
| `-no-open` | Don't open a browser; print the login link        |
| `-v`       | Log every request                                 |
| `-version` | Print the version                                 |
| `-update`  | Update to the latest release                      |

| Shortcut            | Action                        |
|---------------------|-------------------------------|
| `Cmd/Ctrl+S`        | Save (asks for a name if untitled) |
| `Alt+N` / `Alt+W`   | New / close tab               |
| `Alt+P`             | Toggle Markdown Preview / Edit |
| Drag a tab          | Reorder                        |
| Middle-click a tab  | Close                          |

Browsers reserve `Cmd/Ctrl+N` and `W`, hence `Alt`.

## Good to know

- The sidebar shows folders and `.txt`/`.md` files, loading each folder when you
  open it, so even huge folders are instant. Hidden files never show. Files up to 4 MB.
- A name without `.md` gets `.txt`: `notes` → `notes.txt`.
- Lines don't wrap; long lines scroll sideways.
- Open tabs and unsaved text live in `.txt-session.json` in the opened folder
  (private to you). Add it to `.gitignore` in repos; delete it to reset.

## Security

- Listens on `127.0.0.1` only. Each run has a one-time login token, exchanged
  for an `HttpOnly`, `SameSite=Strict` cookie; `Host` and `Origin` are checked.
- Markdown is sanitized with DOMPurify, and a strict Content-Security-Policy
  blocks any script that isn't txt's own.
- File access goes through Go's [`os.Root`](https://pkg.go.dev/os#Root): no
  escaping the folder, not even via symlinks.
- On startup txt asks GitHub if there's a newer release; nothing else is sent.
  Disable with `TXT_NO_UPDATE_CHECK=1`.

## Development

```sh
go test ./...
go run ./cmd/txt -no-open .
```

Release by pushing a tag; GitHub Actions builds every platform:

```sh
git tag v0.1.0 && git push origin v0.1.0
```

```
cmd/txt/            server, flags, security
internal/workspace/ sandboxed file access and session
internal/update/    update check and self-update
web/                UI (plain HTML/CSS/JS, no build step)
web/vendor/         marked, DOMPurify
install.sh          installer
```

## License

[MIT](LICENSE). Bundled fonts ([Lora](https://github.com/cyrealtype/Lora-Cyrillic),
[PT Serif](https://www.paratype.com/public)) are under the SIL Open Font License;
[marked](https://github.com/markedjs/marked) is MIT;
[DOMPurify](https://github.com/cure53/DOMPurify) is MPL-2.0 or Apache-2.0.
Palette inspired by [The Daily Diff](https://tdd.cat).
