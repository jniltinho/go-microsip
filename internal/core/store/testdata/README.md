# Store fixtures

`rust-0.1.0-rc.3.sqlite3` is a database written by the Rust candidate itself (`rust-microsip
0.1.0-rc.3`, schema 5), used by `TestRustCandidateDatabaseOpens`. It was produced in a container
with no network and an empty home directory by importing this fictional configuration:

```toml
schema = 1
[account]
username = "1001"
domain = "pbx.invalid"
server = "pbx.invalid"
port = 5061
transport = "tls"
media = "srtp"
[ui]
theme = "light"
dnd = true
[audio]
input_device = "Lab microphone"
```

```sh
HOME=/w/home ./rust-microsip config import /w/import.toml --apply
# the database is $HOME/.local/share/rust-microsip/rust-microsip.sqlite3
```

It holds no personal data, no password and no call history.
