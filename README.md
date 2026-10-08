# txt

A small, local editor for plain `.txt` files that runs in your browser.

Point `txt` at a folder and it opens a quiet, newspaper-style editor with a file
sidebar and tabs. Everything stays on your machine: the server only listens on
`127.0.0.1`, works offline, and is a single binary with no dependencies.

## Features

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

Requires [Go](https://go.dev/dl/) 1.26 or newer.

```sh
git clone <this repo> txt
cd txt
go build -o txt ./cmd/txt
```

Then move the `txt` binary somewhere on your `PATH`, for example `~/bin`. The
web UI and fonts are embedded in the binary, so it is the only file you need.

## Usage

```sh
txt                 # edit the .txt files in the current folder
txt ~/notes         # edit the .txt files in ~/notes
txt ~/notes/todo.txt  # edit ~/notes, opening todo.txt in a tab
```

txt prints a one-time link and opens it in your browser:

```
txt is serving /Users/you/notes

  http://127.0.0.1:7777/?token=…

Press Ctrl+C to stop.
```

| Flag       | Description                                                   |
|------------|---------------------------------------------------------------|
| `-port N`  | Port to listen on (default `7777`; falls back to a free port if busy) |
| `-no-open` | Don't open a browser; just print the link                     |
| `-v`       | Log every HTTP request                                        |

### Keyboard and mouse

| Action                  | How                                               |
|-------------------------|---------------------------------------------------|
| Save                    | `Cmd+S` / `Ctrl+S` (asks for a name on untitled tabs) |
| New tab                 | `Alt+N`, the `+` in the tab bar, or double-click empty tab-bar space |
| Close tab               | `Alt+W`, the `×` on the tab, or middle-click      |
| Reorder tabs            | Drag a tab                                         |
| New file / folder       | `+` next to **Files** in the sidebar              |
| Insert a tab character  | `Tab`                                              |

New and close use `Alt` because browsers reserve `Cmd/Ctrl+N`, `W` and `T`.

The **New file**, **New folder** and **Save as** dialogs start with the selected
folder's path filled in. Click a folder in the sidebar to select it, or click
empty sidebar space to select the top-level folder.

## What txt shows and saves

- The sidebar lists `.txt` files, and folders that contain them or are empty.
  Hidden files and folders (names starting with `.`) are never shown or opened.
- Files up to 4 MB can be opened.
- Lines don't wrap; long lines scroll horizontally.

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
- All file access goes through Go's [`os.Root`](https://pkg.go.dev/os#Root), so
  paths can't escape the workspace, not even through symlinks.

## Development

```sh
go test ./...
go run ./cmd/txt -no-open .
```

```
cmd/txt/            HTTP server, CLI flags, security middleware
internal/workspace/ sandboxed file access: tree, read, atomic save, session
web/                embedded UI: index.html, style.css, app.js (no build step)
web/fonts/          Lora and PT Serif (SIL Open Font License, see OFL-*.txt)
```

The frontend is plain HTML, CSS and JavaScript with no dependencies or build
step. Edit the files in `web/` and rebuild the binary to see changes.

## License

txt is released under the [MIT License](LICENSE). The bundled fonts keep their
own license, the SIL Open Font License (see below).

## Credits

Typefaces: [Lora](https://github.com/cyrealtype/Lora-Cyrillic) by The Lora
Project Authors and [PT Serif](https://www.paratype.com/public) by ParaType,
both under the [SIL Open Font License 1.1](https://openfontlicense.org).
The colour palette is inspired by [The Daily Diff](https://tdd.cat).
