# CLAUDE.md

Guidance for working on this repository.

## Project

`plurk-cli` is a Go CLI for managing the user's own Plurk timeline through the Plurk API 2.0 (OAuth 1.0a). The primary goal is **deleting the user's own plurks within a date range**, safely.

Scope:
- Only plurks owned by the authorized user (`filter=my`). Responses are out of scope.
- Single-user tool. Credentials live in a local config file.

## Commands

```sh
go build -o plurk .     # build
go test ./...           # run tests
go vet ./...            # static checks
gofmt -l .              # list unformatted files (should print nothing)
```

## Tech stack

- Go (module `github.com/z6wdc/plurk-cli`)
- CLI: `github.com/spf13/cobra`
- OAuth 1.0a: `github.com/dghubble/oauth1`
- Everything else from the standard library. Do not add dependencies without a clear reason.

## Architecture

```
main.go                     # calls cmd.Execute()
cmd/                        # cobra commands: flag parsing and output ONLY
  root.go                   # root command, global flags (--config, --verbose)
  login.go                  # OAuth PIN flow, saves credentials
  whoami.go                 # GET Users/me
  list.go                   # list own plurks in range
  delete.go                 # delete own plurks in range
internal/
  config/config.go          # Config struct, Load/Save, default path
  plurk/
    client.go               # OAuth1-signed HTTP client, shared call(), error decoding, retry
    auth.go                 # RequestToken, AuthorizeURL, AccessToken
    types.go                # Plurk, User structs; Plurk time parsing/formatting
    timeline.go             # Me, GetPlurks, DeletePlurk
  timerange/range.go        # parse --from/--to; iterate plurks within a range
```

Rules:
- Business logic belongs in `internal/`. `cmd/` wires flags to `internal/` calls and prints results.
- `internal/plurk` should be a thin API wrapper with no knowledge of date ranges or dry-run.
- Write output to `cmd.OutOrStdout()` / `cmd.ErrOrStderr()` so commands are testable.

## Config

- Path: `filepath.Join(os.UserConfigDir(), "plurk-cli", "config.json")`, overridable with `--config`.
- Fields: `app_key`, `app_secret`, `access_token`, `access_token_secret`.
- Create the directory with mode `0700` and write the file with mode `0600`.
- `login` reads `PLURK_APP_KEY` / `PLURK_APP_SECRET` from the environment if set, and prompts otherwise. Pressing Enter at a prompt keeps the saved value, which is never echoed.

## Plurk API notes

Docs: <https://www.plurk.com/API>

- API base URL: `https://www.plurk.com/APP/`
- OAuth endpoints:
  - Request token: `https://www.plurk.com/OAuth/request_token`
  - Authorize: `https://www.plurk.com/OAuth/authorize?oauth_token=...`
  - Access token: `https://www.plurk.com/OAuth/access_token`
  - Use `oauth_callback=oob`. The user pastes the PIN as `oauth_verifier`.
- Requests are form-encoded POSTs signed with OAuth 1.0a (HMAC-SHA1).
- Errors come back as non-2xx responses with a body like `{"error_text": "..."}`. Decode them into a typed error.
- `Client.call` retries HTTP 429 and 5xx with exponential backoff (1s, 2s; 3 attempts total) for every endpoint. Other errors are returned immediately.

Endpoints used:

| Endpoint               | Params                         | Notes                                             |
|------------------------|--------------------------------|---------------------------------------------------|
| `Users/me`             | none                           | Returns the authorized user, including `id`       |
| `Timeline/getPlurks`   | `offset`, `limit`, `filter=my` | Returns `{"plurks": [...], "plurk_users": {...}}` |
| `Timeline/plurkDelete` | `plurk_id`                     | Returns `{"success_text": "ok"}`                  |

Plurk fields we rely on: `plurk_id`, `owner_id`, `posted`, `content_raw`, `content`, `qualifier`.

Time formats:
- `offset` param: `2006-01-02T15:04:05` in **UTC**. Results are plurks **older than** `offset`, newest first.
- `posted` field: RFC1123 in GMT, e.g. `Fri, 05 Jun 2009 23:07:13 GMT` (`time.RFC1123`).

## Core flows

### Iterating plurks in a range (`internal/timerange`)

1. Parse `--from` / `--to` as `YYYY-MM-DD` in local time. The range is `[from 00:00, to+1day 00:00)`.
2. Initial `offset` = range end converted to UTC. Call `getPlurks` with `filter=my`, `limit=30`.
3. For each plurk on the page:
   - skip it if `posted` is at or after the range end;
   - **stop entirely** if `posted` is before the range start;
   - otherwise yield it.
4. Next `offset` = `posted` of the last plurk on the page **plus one second** (or exactly `posted` if that would not move backwards). Offsets have one-second precision, so the overlap keeps plurks that share the last timestamp from being skipped. Track seen `plurk_id`s to drop the duplicates. Stop when a page is empty.
5. Only yield plurks where `owner_id == Me().id`, as a safety net.
6. Guard against infinite loops: if the last plurk on a page is not older than the offset used to fetch it, stop with an error.

Implemented as `timerange.Plurks(ctx, lister, range, ownerID) iter.Seq2[plurk.Plurk, error]`, shared by `list` and `delete`.

### Deleting (`cmd/delete.go`)

1. Collect **all** matching plurks first. Never delete while paginating, because deleting shifts what later pages return.
2. Print a summary: count plus the oldest and newest `posted`.
3. Without `--yes`, print the list and exit. This is the dry-run.
4. With `--yes`, for each plurk:
   - append a JSONL record (`plurk_id`, `posted`, `content_raw`, `deleted_at`) to the log and flush it **before** deleting;
   - call `plurkDelete`;
   - sleep `--delay` (default 500ms);
   - on HTTP 429 or 5xx, retry with exponential backoff, up to 3 attempts.
5. Print success and failure counts, and list failed IDs at the end. Exit non-zero if anything failed.
6. Respect context cancellation (Ctrl-C) between deletions.

## Conventions

- Wrap errors with context: `fmt.Errorf("get plurks: %w", err)`.
- Every function that makes an HTTP call takes a `context.Context` as its first argument.
- Inject the base URL and `*http.Client` into the Plurk client so tests can point it at `httptest.Server`.
- Tests must **never** call the real Plurk API. Use `httptest.Server` fakes.
- Use table-driven tests for parsing (dates, `posted`) and for pagination edge cases.
- `cmd` tests override package variables (`apiBaseURL`, `retryBackoff`, `localZone`) instead of globals like `time.Local`, and reset flags and contexts between runs (`resetCommands`). Run `go test -race ./...` before finishing.

## Safety rules (do not violate)

- Destructive operations default to dry-run. Never remove or weaken the `--yes` requirement.
- Always write the log record before the delete request.
- Never print or log tokens or secrets, including in `--verbose` output. Redact the `Authorization` header.
- Never commit config files or credentials. `.gitignore` should cover `config.json` and `*.jsonl`.
