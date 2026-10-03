# plurk-cli

A command-line tool for managing your own [Plurk](https://www.plurk.com) timeline via the Plurk API 2.0 (OAuth 1.0a).

Its main feature is **bulk-deleting your own plurks within a date range**, with safe defaults: dry-run first, plus a JSONL log that doubles as a backup.

## Features

- `plurk login`: authorize the CLI with your Plurk account (OAuth PIN flow)
- `plurk whoami`: check that your credentials work
- `plurk list`: list your own plurks within a date range
- `plurk delete`: delete your own plurks within a date range (dry-run by default)

## Prerequisites

1. A Plurk account.
2. A registered Plurk app:
   - Go to <https://www.plurk.com/PlurkApp/> and register a new app.
   - You can leave the callback URL empty; the CLI uses the PIN (`oob`) flow.
   - Note down the **App Key** and **App Secret**.
3. Go 1.23 or later (only if building from source).

## Installation

```sh
go install github.com/z6wdc/plurk-cli@latest
```

Or build from source:

```sh
git clone https://github.com/z6wdc/plurk-cli.git
cd plurk-cli
go build -o plurk .
```

## Quick start

```sh
# 1. Authorize. You will be prompted for the App Key/Secret, then given a URL
#    to open in your browser. Paste the PIN shown by Plurk back into the terminal.
plurk login

# 2. Verify
plurk whoami

# 3. See what is in the range
plurk list --from 2015-01-01 --to 2015-12-31

# 4. Dry-run: shows what WOULD be deleted, deletes nothing
plurk delete --from 2015-01-01 --to 2015-12-31

# 5. Actually delete
plurk delete --from 2015-01-01 --to 2015-12-31 --yes
```

## Commands

| Command                                      | Description                                                    |
|----------------------------------------------|----------------------------------------------------------------|
| `plurk login`                                | Run the OAuth PIN flow and save credentials to the config file |
| `plurk whoami`                               | Show the authorized account (display name, nick name, user id) |
| `plurk list --from DATE --to DATE`           | List your own plurks posted within the range                   |
| `plurk delete --from DATE --to DATE [flags]` | Delete your own plurks posted within the range                 |

Dates use the `YYYY-MM-DD` format and your local time zone. Both ends are inclusive: `--to 2015-12-31` covers the whole of Dec 31.

### `delete` flags

| Flag               | Default                           | Description                                                                 |
|--------------------|-----------------------------------|-----------------------------------------------------------------------------|
| `--yes`            | `false`                           | Actually delete. Without it, the command only prints what would be deleted. |
| `--log PATH`       | `plurk-deleted-<timestamp>.jsonl` | JSONL file that records every plurk before it is deleted                    |
| `--delay DURATION` | `500ms`                           | Pause between delete requests                                               |

Rate-limit (HTTP 429) and server (5xx) errors are retried automatically, up to 3 attempts per request. If a plurk still cannot be deleted, `delete` moves on to the next one, lists the failures at the end, and exits with a non-zero status. Press Ctrl-C to stop after the current plurk.

### Global flags

| Flag            | Description                                |
|-----------------|--------------------------------------------|
| `--config PATH` | Use a different config file                |
| `--verbose`     | Print HTTP requests and other debug output |

## Configuration

Credentials are stored in `<user config dir>/plurk-cli/config.json` with file mode `0600`:

- macOS: `~/Library/Application Support/plurk-cli/config.json`
- Linux: `~/.config/plurk-cli/config.json`
- Windows: `%AppData%\plurk-cli\config.json`

If these environment variables are set, `plurk login` reads the App Key/Secret from them instead of prompting. When prompted, pressing Enter keeps the value already saved in the config file.

| Variable           | Description           |
|--------------------|-----------------------|
| `PLURK_APP_KEY`    | App (consumer) key    |
| `PLURK_APP_SECRET` | App (consumer) secret |

## Safety notes

- **Deletion is permanent.** Plurk has no trash bin. Always run without `--yes` first and review the list.
- Every plurk is appended to the `--log` file **before** it is deleted. Keep this file; it is your only backup. Each line holds the plurk ID, URL, posted time, raw and HTML content, and deletion time. The file is created with mode `0600`.
- Never commit your config file. It holds secrets that give full access to your account.

## Limitations

- Only plurks **you posted** are handled. Your responses on other people's plurks are not deleted.
- The Plurk API is rate limited. Large deletions take a while, and you can raise `--delay` if you hit errors.
- If a run is interrupted or some deletions failed, re-run the same command. Plurks that were already deleted will not show up again.
