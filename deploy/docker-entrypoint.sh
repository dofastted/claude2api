#!/bin/sh
set -e

# Fix data directory permissions when running as root.
# Docker named volumes / host bind-mounts may be owned by root,
# preventing the non-root claude2api user from writing files.
if [ "$(id -u)" = "0" ]; then
    mkdir -p /app/data
    # Use || true to avoid failure on read-only mounted files (e.g. config.yaml:ro)
    chown -R claude2api:claude2api /app/data 2>/dev/null || true
    # Re-invoke this script as claude2api so the flag-detection below
    # also runs under the correct user.
    exec su-exec claude2api "$0" "$@"
fi

# Compatibility: if the first arg looks like a flag (e.g. --help),
# prepend the default binary so it behaves the same as the old
# ENTRYPOINT ["/app/claude2api"] style.
if [ "${1#-}" != "$1" ]; then
    set -- /app/claude2api "$@"
fi

exec "$@"
