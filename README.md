# txt

A small, local editor for plain-text (`.txt`) and Markdown (`.md`) files that
runs in your browser.

Point `txt` at a folder and it opens a quiet, newspaper-style editor with a file
sidebar and tabs. Everything stays on your machine: the server only listens on
`127.0.0.1`, works offline, and is a single binary with no dependencies.

## Features

- **Markdown preview**: `.md` files open rendered; switch to Edit to see the
  text and a live preview side by side.
- **Tabs**: open several files at once, drag to reorder, and start writing
  straight away in an `untitled.txt` tab.
- **Session restore**: open tabs, unsaved edits, cursor and scroll position
  come back after a reload or restart.
- **Safe saves**: every save is atomic (write, fsync, rename). If a file changed
  on disk since you opened it, txt refuses to overwrite it and tells you.
- **Plain text, respected**: line endings (LF/CRLF) and file permissions are
  preserved, a trailing newline is ensured, and only valid UTF-8 is opened.
- **Light and dark themes**: follows your system setting, with a toggle in the
  sidebar.
- **Sandboxed to one folder**: the editor can't read or write anything outside
  the folder you started it on.

## Install

On macOS or Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/likhithkr7/txt/main/install.sh | sh
```

The script downloads the right binary for your machine from
[GitHub Releases](https://github.com/likhithkr7/txt/releases), checks its
SHA-256 checksum, and installs it to `~/.local/bin` (or another writable folder
already on your `PATH`), adding that folder to your `PATH` if needed. Run it
again to update. Options, set as environment variables:

```sh
curl -fsSL https://raw.githubusercontent.com/likhithkr7/txt/main/install.sh | VERSION=0.1.0 sh
curl -fsSL https://raw.githubusercontent.com/likhithkr7/txt/main/install.sh | INSTALL_DIR=~/bin sh
```

On Windows, download `txt-<version>-windows-amd64.exe` (or `-arm64.exe`) from the
[releases page](https://github.com/likhithkr7/txt/releases), rename it to
`txt.exe`, and put it in a folder on your `PATH`.

To uninstall, delete the binary: `rm "$(command -v txt)"`.

### From source

Requires [Go](https://go.dev/dl/) 1.26 or newer.

```sh
git clone https://github.com/likhithkr7/txt.git
cd txt
go build -o txt ./cmd/txt
```

The web UI, fonts and libraries are embedded in the binary, so it is the only
file you need.

## Usage

```sh
txt                   # edit the .txt and .md files in the current folder
txt ~/notes           # edit the files in ~/notes
txt ~/notes/todo.txt  # edit ~/notes, opening todo.txt in a tab
```

txt opens your browser and logs you in:

```
txt is serving /Users/you/notes

  Opened http://127.0.0.1:7777 in your browser.

Press Ctrl+C to stop.
```

With `-no-open` (or if no browser can be opened), txt prints the full login
link instead, including this run's one-time token.

| Flag       | Description                                                   |
|------------|---------------------------------------------------------------|
| `-port N`  | Port to listen on (default `7777`; falls back to a free port if busy) |
| `-no-open` | Don't open a browser; print the login link instead           |
| `-v`       | Log every HTTP request                                        |
| `-version` | Print the version and exit                                   |

### Keyboard and mouse

| Action                  | How                                               |
|-------------------------|---------------------------------------------------|
| Save                    | `Cmd+S` / `Ctrl+S` (asks for a name on untitled tabs) |
| New tab                 | `Alt+N`, the `+` in the tab bar, or double-click empty tab-bar space |
| Close tab               | `Alt+W`, the `×` on the tab, or middle-click      |
| Reorder tabs            | Drag a tab                                         |
| Markdown view           | `Alt+P` toggles Preview / Edit, or use the switch in the tab bar |
| New file / folder       | `+` next to **Files** in the sidebar              |
| Insert a tab character  | `Tab`                                              |

`Alt` shortcuts are used because browsers reserve `Cmd/Ctrl+N`, `W` and `T`.

The **New file**, **New folder** and **Save as** dialogs start with the selected
folder's path filled in. Click a folder in the sidebar to select it, or click
empty sidebar space to select the top-level folder.

## What txt shows and saves

- The sidebar lists `.txt` and `.md` files, and folders that contain them or
  are empty. Hidden files and folders (names starting with `.`) are never shown
  or opened.
- New files are plain text unless you name them `.md`: `notes` becomes
  `notes.txt`, and an unknown extension is kept as part of the name
  (`meeting.2026` becomes `meeting.2026.txt`).
- Files up to 4 MB can be opened.
- Lines don't wrap; long lines scroll horizontally.
- In the Markdown preview, web links open in a new browser tab and relative
  links to `.txt`/`.md` files open in txt. Images from the web are shown;
  images stored in your folder are not.

### The session file

Open tabs and unsaved text are stored in `.txt-session.json` at the top of the
folder you run txt on. It is hidden from the sidebar, readable only by you
(mode `600`), and written about a second after each change. If the folder is
under version control, add it to your ignore file:

```sh
echo ".txt-session.json" >> .gitignore
```

Delete the file to start with a clean session.

## Security

txt is meant for one person on one machine.

- It listens on `127.0.0.1` only, never on your network.
- Each run generates a random token. The browser exchanges it once for an
  `HttpOnly`, `SameSite=Strict` cookie, and every API call requires that cookie.
- The `Host` header is checked to block DNS-rebinding attacks, and the `Origin`
  header is checked on every change to block cross-site requests.
- Markdown previews are sanitized with [DOMPurify](https://github.com/cure53/DOMPurify),
  and a strict Content-Security-Policy allows only txt's own scripts, so a
  malicious `.md` file can't run code in the editor.
- All file access goes through Go's [`os.Root`](https://pkg.go.dev/os#Root), so
  paths can't escape the workspace, not even through symlinks.

## Development

```sh
go test ./...
go run ./cmd/txt -no-open .
```

### Releasing

Push a version tag and GitHub Actions builds every platform and publishes the
release that `install.sh` downloads (see `.github/workflows/release.yml`):

```sh
git tag v0.1.0
git push origin v0.1.0
```

```
cmd/txt/            HTTP server, CLI flags, security middleware
install.sh          one-line installer (downloads a release binary)
.github/workflows/  release build: binaries for macOS, Linux, Windows
internal/workspace/ sandboxed file access: tree, read, atomic save, session
web/                embedded UI: index.html, style.css, app.js, theme.js (no build step)
web/fonts/          Lora and PT Serif (SIL Open Font License, see OFL-*.txt)
web/vendor/         marked and DOMPurify, unmodified release files (see LICENSE-*.txt)
```

The frontend is plain HTML, CSS and JavaScript with no build step; its two
libraries are checked in under `web/vendor/`. Edit the files in `web/` and rebuild the binary to see changes.

## License

txt is released under the [MIT License](LICENSE). The bundled fonts and
libraries keep their own licenses (see Credits).

## Credits

Typefaces: [Lora](https://github.com/cyrealtype/Lora-Cyrillic) by The Lora
Project Authors and [PT Serif](https://www.paratype.com/public) by ParaType,
both under the [SIL Open Font License 1.1](https://openfontlicense.org).
Markdown rendering by [marked](https://github.com/markedjs/marked) (MIT) and
sanitizing by [DOMPurify](https://github.com/cure53/DOMPurify) (MPL-2.0 or
Apache-2.0). The colour palette is inspired by [The Daily Diff](https://tdd.cat).
