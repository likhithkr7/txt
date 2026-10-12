# txt

A local, browser-based editor for text, Markdown and CSV files. Files of any
size open instantly; those over 4 MB are view-only. One binary, works offline,
listens only on `127.0.0.1`.

## Features

- **Any size, instantly**: files over 4 MB open view-only and read only what's on screen, so a 4 GB CSV opens as fast as a note.
- **Fast search**: find next/previous and a match count across the whole file, searched in parallel chunks.
- **Text, Markdown, CSV, TSV, logs**: `.md` files open rendered; **Edit** shows live preview side by side.
- **Tabs and session restore**: tabs and unsaved text survive reloads and restarts.
- **Safe saves**: atomic writes; never overwrites a file changed on disk.
## Install

**macOS / Linux**

```sh
curl -fsSL https://raw.githubusercontent.com/likhithkr7/txt/main/install.sh | sh
```

**Windows**: download `txt-<version>-windows-amd64.exe` from
[Releases](https://github.com/likhithkr7/txt/releases), rename it to `txt.exe`,
and put it on your `PATH`.

**Update**: `txt -update`

## Usage

```sh
txt                   # current folder
txt ~/data            # any folder
txt ~/data/trades.csv # a folder, opening one file
```

| Shortcut                  | Action                      |
|---------------------------|-----------------------------|
| `Cmd/Ctrl+S`              | Save                        |
| `Cmd/Ctrl+F`              | Search (view-only files)    |
| `Enter` / `Shift+Enter`   | Next / previous match       |
| `Alt+N` / `Alt+W`         | New / close tab             |
| `Alt+P`                   | Toggle Markdown preview     |

Flags: `-port N`, `-no-open`, `-v`, `-version`, `-update`.

## Good to know

- Opens `.txt`, `.md`, `.csv`, `.tsv` and `.log` files.
- Files over 4 MB, or not valid UTF-8, are view-only.
- Search ignores case for A–Z unless you press **Aa**.
- Open tabs live in `.txt-session.json` in the opened folder; delete it to reset.
