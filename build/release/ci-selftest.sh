#!/bin/sh
# Prove that the CI gates block injected failures, using temporary copies of
# the committed tree only (nothing is committed or pushed).
#   Publication scan: a clean copy passes; a staged base/ path, a fictional
#      lab password, private key, token (also inside a binary or UTF-16 file,
#      or only in history), personal home path, literal TOML password or
#      secret-named file fails, in the Git index, in history and in an
#      artifact directory.
#      The owner-approved exceptions stay narrow: another database beside the
#      allowed fixture, and a history-only name staged again, fail.
# Usage: sh build/release/ci-selftest.sh
set -eu
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
scan="$here/scan-publication.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

# Fictional values, assembled at runtime so this file never matches the scan.
dashes=-----
fake_key=$(printf '%sBEGIN %s %s%s' "$dashes" PRIVATE KEY "$dashes")
fake_token=$(printf 'gh%s_%s' p "$(printf 'Z%.0s' $(seq 36))")
fake_home=$(printf '/%s/%s/' home alice-fictional)
fake_password=$(printf 'fictional-lab-%s-%s' password 0123456789abcdef)
fake_word=fictional-config-value
example_marker=$(printf 'publication-scan: %s' fictional-example)
mkdir -p "$work/lab/clients"
printf '{"1001": "%s"}\n' "$fake_password" > "$work/lab/clients/credentials.json"

fresh_copy() { # committed tree, every file staged, no history
    rm -rf "$work/copy"
    mkdir "$work/copy"
    git -C "$repo" archive HEAD | tar -x -C "$work/copy"
    git -C "$work/copy" init --quiet
    # No detached auto-gc/maintenance: it races with the next removal.
    git -C "$work/copy" config gc.auto 0
    git -C "$work/copy" config maintenance.auto false
    git -C "$work/copy" add -A
}
commit() { # message
    git -C "$work/copy" -c user.name=Selftest -c user.email=selftest@example.invalid \
        -c commit.gpgsign=false commit --quiet --allow-empty -m "$1"
}
expect_pass() { # name, scan arguments...
    name=$1; shift
    if (cd "$work/copy" && LAB_DIR="$work/lab" sh "$scan" "$@") > "$work/log" 2>&1; then
        echo "ok   $name: passed"
    else
        cat "$work/log"; echo "FAIL $name: clean input was rejected" >&2; exit 1
    fi
}
expect_block() { # name, expected category, scan arguments...
    name=$1; category=$2; shift 2
    if (cd "$work/copy" && LAB_DIR="$work/lab" sh "$scan" "$@") > "$work/log" 2>&1; then
        cat "$work/log"; echo "FAIL $name: injected failure was not blocked" >&2; exit 1
    fi
    grep -q "^FAIL \[$category\]" "$work/log" || {
        cat "$work/log"; echo "FAIL $name: blocked for another reason" >&2; exit 1; }
    ! grep -q -F -e "$fake_password" -e "$fake_token" "$work/log" || {
        echo "FAIL $name: scan output leaked a secret value" >&2; exit 1; }
    echo "ok   $name: blocked ($category)"
}

fresh_copy
expect_pass "clean committed tree"
mkdir "$work/copy/base"; echo fictional > "$work/copy/base/notes.txt"
git -C "$work/copy" add -f base/notes.txt
expect_block "staged base/ path" forbidden-path
fresh_copy
echo fictional > "$work/copy/.env"; git -C "$work/copy" add -f .env
expect_block "staged .env file" forbidden-path
fresh_copy
echo "password: $fake_password" > "$work/copy/leak.txt"; git -C "$work/copy" add leak.txt
expect_block "staged lab password" lab-secret
fresh_copy
echo "$fake_key" > "$work/copy/leak.txt"; git -C "$work/copy" add leak.txt
expect_block "staged private key" private-key
fresh_copy
echo "token=$fake_token" > "$work/copy/leak.txt"; git -C "$work/copy" add leak.txt
expect_block "staged token" token
fresh_copy
echo "see ${fake_home}project" > "$work/copy/leak.txt"; git -C "$work/copy" add leak.txt
expect_block "staged personal home path" personal-home
fresh_copy
echo "see file://${fake_home}project" > "$work/copy/leak.txt"; git -C "$work/copy" add leak.txt
expect_block "staged file:// home URL" personal-home
fresh_copy
echo "cc -L${fake_home}lib" > "$work/copy/leak.txt"; git -C "$work/copy" add leak.txt
expect_block "staged -L home flag" personal-home
fresh_copy
printf 'bin\000ary token=%s\n' "$fake_token" > "$work/copy/leak.bin"; git -C "$work/copy" add leak.bin
expect_block "staged token in binary file" token
fresh_copy
printf 'token=%s\n' "$fake_token" | iconv -f UTF-8 -t UTF-16LE > "$work/copy/leak.txt"
git -C "$work/copy" add leak.txt
expect_block "staged token in UTF-16 file" token
fresh_copy
printf '[account]\n%s = "%s"\n' password "$fake_word" > "$work/copy/config.toml"
git -C "$work/copy" add config.toml
expect_block "staged literal TOML password" app-password
mkdir -p "$work/copy/docs"; mv "$work/copy/config.toml" "$work/copy/docs/config.toml"
git -C "$work/copy" add -A
expect_block "unmarked TOML password under docs/" app-password
printf '# %s\n' "$example_marker" >> "$work/copy/docs/config.toml"; git -C "$work/copy" add -A
expect_pass "marked TOML example under docs/"
fresh_copy
echo fictional > "$work/copy/db-password.txt"; git -C "$work/copy" add db-password.txt
expect_block "staged secret-named file" secret-file-name
fresh_copy
echo "token=$fake_token" > "$work/copy/leak.txt"; git -C "$work/copy" add leak.txt
commit "fictional leak"
git -C "$work/copy" rm --quiet leak.txt; commit "remove fictional leak"
expect_block "token only in history" token
fresh_copy
echo fictional > "$work/copy/.env"; git -C "$work/copy" add -f .env; commit "fictional env"
git -C "$work/copy" rm --quiet --cached .env; rm "$work/copy/.env"; commit "remove fictional env"
expect_block "forbidden path only in history" forbidden-path
fresh_copy
echo x > "$work/copy/internal/core/store/testdata/other.sqlite3"
git -C "$work/copy" add -f internal/core/store/testdata/other.sqlite3
expect_block "a database beside the allowed fixture" forbidden-path
fresh_copy
echo x >> "$work/copy/internal/core/store/testdata/rust-0.1.0-rc.3.sqlite3"
git -C "$work/copy" add internal/core/store/testdata/rust-0.1.0-rc.3.sqlite3
expect_block "the allowed fixture with other content" forbidden-path
fresh_copy
echo fictional > "$work/copy/db-password.txt"; git -C "$work/copy" add db-password.txt; commit "fictional name"
git -C "$work/copy" rm --quiet db-password.txt; commit "remove fictional name"
expect_block "an unrelated secret-named file only in history" secret-file-name
# A historical blob exempt from the personal-home check is exempt only while it is history.
exempt_blob=47ffedc4c66df172626c51518a8e29f32eb3bc90
if git -C "$repo" cat-file -e "$exempt_blob" 2> /dev/null; then
    fresh_copy
    git -C "$repo" cat-file blob "$exempt_blob" > "$work/copy/old-store.txt"
    git -C "$work/copy" add old-store.txt
    expect_block "an exempt historical blob back in the index" personal-home
else
    echo "skip an exempt historical blob back in the index: this clone does not have the blob"
fi
fresh_copy
mkdir -p "$work/copy/internal/platform/secrets"
echo fictional > "$work/copy/internal/platform/secrets/secrets.go"
git -C "$work/copy" add internal/platform/secrets/secrets.go
expect_block "a history-only exempt name in the index" secret-file-name

fresh_copy
mkdir "$work/dist"; echo 'fictional artifact' > "$work/dist/go-microsip"
expect_pass "clean artifact directory" "$work/dist"
printf 'prefix\000%s\000suffix' "$fake_password" > "$work/dist/go-microsip"
expect_block "lab password inside binary artifact" lab-secret "$work/dist"
echo 'fictional artifact' > "$work/dist/go-microsip"; echo x > "$work/dist/profile.sqlite3"
expect_block "database in artifact directory" forbidden-path "$work/dist"
rm "$work/dist/profile.sqlite3"
printf 'token=%s\n' "$fake_token" | iconv -f UTF-8 -t UTF-16LE > "$work/dist/notes.txt"
expect_block "UTF-16 token in artifact directory" token "$work/dist"
rm "$work/dist/notes.txt"
# Compressed Windows packages are scanned unpacked (zip members, MSI payload).
zip_with() { python3 -I -c 'import sys, zipfile
with zipfile.ZipFile(sys.argv[1], "w", zipfile.ZIP_DEFLATED) as z: z.writestr(sys.argv[2], sys.argv[3])' "$work/dist/go-microsip.zip" "$@"; }
zip_with go-microsip/README.txt "see ${fake_home}project"
expect_block "personal home path inside a zip member" personal-home "$work/dist"
zip_with go-microsip/.env fictional
expect_block "forbidden path inside a zip" forbidden-path "$work/dist"
zip_with go-microsip/notes.txt "token=$fake_token"
expect_block "token inside a zip member" token "$work/dist"
rm "$work/dist/go-microsip.zip"
if ! command -v msiextract > /dev/null; then
    echo fictional > "$work/dist/go-microsip.msi"
    expect_block "MSI without msitools fails closed" msitools-missing "$work/dist"
    rm "$work/dist/go-microsip.msi"
fi
echo "scanner selftest passed"
