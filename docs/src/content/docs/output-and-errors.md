---
title: Output and errors
description: Integrate bitbucket-cli reliably into scripts and agent workflows.
---

## JSON output

Commands emit JSON by default. List commands return arrays of full Bitbucket
objects; single-resource commands return the entity object. Pretty output is
opt-in:

```sh
bitbucket-cli pr list          # JSON on stdout
bitbucket-cli pr list --pretty # one summary line per pull request
```

Errors are written to stderr:

```json
{
  "error": {
    "message": "Bitbucket resource not found.",
    "status": 404,
    "method": "GET",
    "url": "https://api.bitbucket.org/2.0/...",
    "excerpt": "..."
  }
}
```

Configuration and usage errors contain only a message. HTTP errors include
status, method, URL, and a bounded response excerpt.

## Exit behavior

- Exit `0` indicates success.
- Exit `1` indicates an API, network, authentication, configuration, or usage
  failure.

Parse stdout and stderr separately. The API token is never included in rendered
output or error messages.

## Failure handling

Authentication, authorization, not-found, rate-limit, and network errors are
surfaced without automatic retries. Inspect the error and correct credentials,
repository resolution, permissions, or request parameters before trying again.
Remote writes are not safe to retry blindly.
