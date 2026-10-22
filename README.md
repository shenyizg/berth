# berth

[![CI](https://github.com/shenyizg/berth/actions/workflows/ci.yml/badge.svg)](https://github.com/shenyizg/berth/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

English | [简体中文](README.zh-CN.md)

**berth is `ssh` with automatic port forwarding.** It watches the remote host
for TCP ports that start listening and forwards each one to your machine as
soon as it appears. A server you start over SSH is reachable at `localhost`
right away, in any terminal, with no editor or extra setup.

```console
$ berth dev.example.com
me@dev:~$ npm run dev

  VITE v5.4.0  ready in 412 ms

  ➜  Local:   http://localhost:5173/
berth: forwarded remote port 5173 to localhost:5173
```

Open <http://localhost:5173> on your own machine, and you are talking to the
server on `dev.example.com`.

## Features

- **Drop-in for ssh.** berth runs your own `ssh` with your arguments, so
  `~/.ssh/config`, keys, agents, jump hosts and every ssh option keep working.
- **Automatic.** New listening ports are forwarded within about a second, and
  their forwards go away when they close.
- **Same port numbers.** A remote port is forwarded to the same port on your
  machine, so URLs printed by remote programs work as they are, including
  login flows that send your browser to `http://localhost:<port>`. If the port
  is taken locally, berth picks a free one and tells you which.
- **Only your ports.** By default, berth forwards the ports of the user you log
  in as, not every daemon on the server.
- **Nothing to install on the server.** berth sends a short POSIX shell script
  over the existing connection. It works on Linux, macOS and other Unix-like
  systems.
- **One connection, one login.** Everything goes through OpenSSH connection
  sharing, so you authenticate once. Forwards listen on your loopback
  addresses only.
- **Cleans up after itself.** When you log out, the forwards and the script on
  the server go away, even if berth itself is killed.

## Install

With Go 1.24 or newer:

```sh
go install github.com/shenyizg/berth/cmd/berth@latest
```

This puts `berth` in `~/go/bin` (more precisely, `$(go env GOPATH)/bin`). If
your shell then says `command not found`, add that directory to your `PATH` in
your shell's startup file, such as `~/.zshrc` or `~/.bashrc`, and open a new
terminal:

```sh
export PATH="$PATH:$HOME/go/bin"
```

You can also download a binary from the
[releases page](https://github.com/shenyizg/berth/releases) and put it in a
directory on your `PATH`, or build from a clone of this repository with
`make build`.

berth needs the OpenSSH client on your machine: macOS, Linux, the BSDs and WSL
all work. (The Windows port of OpenSSH lacks connection sharing.) The server
needs `/bin/sh` and must allow TCP forwarding, which is the default.

## Usage

```
berth [berth options] [ssh options] destination [command]
```

Use berth wherever you would use ssh. berth's own options are long options
that come first; all other arguments go to ssh unchanged.

```sh
berth dev.example.com                    # a shell, with ports forwarded as they open
berth -p 2222 -J bastion me@10.0.0.7     # any ssh options work
berth dev 'cd app && npm run dev'        # forward ports while a command runs
berth -N dev                             # no shell: forward ports until Ctrl-C
berth --exclude 5432,6379 dev            # never forward these ports
berth --include 8888 dev                 # also forward a port that was open before
```

To use berth for all your interactive connections, add an alias to your
shell's startup file:

```sh
alias ssh=berth
```

Programs such as git, scp and rsync keep running the real ssh.

### Options

| Option | Description |
| --- | --- |
| `--scope MODE` | Which listening ports to forward: `auto` (default), `mine`, `new`, `all` or `none`. See below. |
| `--include PORTS` | Always forward these ports when they listen, e.g. `8888,9000-9010`. |
| `--exclude PORTS` | Never forward these ports. Takes precedence over `--include`. |
| `--same-port` | Only forward a port to the same number locally; skip ports that are busy on your machine. |
| `--interval TIME` | How often the server is checked for listening ports (default `1s`). |
| `--notify MODE` | How to report new forwards: `terminal` (default), `desktop` or `none`. |
| `--quiet` | Same as `--notify none`. |
| `--ssh PROGRAM` | The ssh program to run (default: `$BERTH_SSH`, or `ssh`). |
| `--verbose` | Explain what berth is doing. |
| `--version` | Print the version. |
| `-h`, `--help` | Print a summary of the options. |

### Which ports are forwarded

| Scope | Forwards |
| --- | --- |
| `mine` | Ports opened by the user you log in as, including ones that were already listening when you connected. The server must report who owns each socket, which Linux and macOS do. |
| `new` | Ports that start listening after you connect. |
| `all` | Every listening port. |
| `none` | Only the ports given with `--include`. |
| `auto` | `mine` if the server reports socket owners and you do not log in as root, `new` otherwise. |

`--include` and `--exclude` apply in every scope. berth leaves out the ports
of your own `-R` forwards, and X11 display ports when you use `-X` or `-Y`.

### Messages

berth reports new forwards on the terminal:

```
berth: forwarded remote port 3000 to localhost:3000
berth: forwarded remote port 5000 to localhost:61202 (local port 5000 is not available)
```

These lines can land in the middle of full-screen programs. Use
`--notify desktop` to get desktop notifications instead (on macOS, or Linux
with `notify-send`), or `--quiet` to turn them off.

## How it works

1. berth runs `ssh` with connection sharing (`ControlMaster` and
   `ControlPersist`) on a private control socket. You log in as usual.
2. Over the same connection, berth opens a second session that runs a small
   shell script. Every second, the script lists the listening TCP sockets,
   from `/proc/net/tcp` on Linux or with `lsof`, `ss` or `netstat` elsewhere,
   and reports any change.
3. For each new port, berth adds a forward to the connection, as
   `ssh -O forward -L localhost:PORT:127.0.0.1:PORT` would.
4. When your session ends, berth closes the connection, which ends the script
   and removes the forwards.

[docs/how-it-works.md](docs/how-it-works.md) has the details.

## Troubleshooting

**Nothing is forwarded.** Run berth with `--verbose` to see what it finds on
the server. Common causes:

- The server forbids forwarding (`AllowTcpForwarding no` in `sshd_config`).
  The local port opens, but connections to it fail.
- With the `new` scope, ports that were listening before you connected are not
  forwarded. Add them with `--include`, or use `--scope all`.
- You log in as root, so `auto` means `new`.

**A port appears under a different number.** The port is in use on your
machine, so berth picked a free one. Use `--same-port` to skip such ports
instead.

**berth says the host "is this machine".** You connected to your own machine,
or to a container that shares its network. Its ports are reachable already, so
berth does not forward them.

**The server runs Windows.** That is not supported: the script needs a POSIX
shell.

## Security

Forwards listen on your machine's loopback addresses (127.0.0.1 and ::1) only,
never on your network, whatever `GatewayPorts` says. As with `ssh -L`, other
programs and users on your machine can connect to them. The control socket is
created in a private temporary directory.

## License

berth is released under the [MIT license](LICENSE).
