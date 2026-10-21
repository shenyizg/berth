# How berth works

This document describes what berth does under the hood: which processes it
starts, how it finds listening ports on the server, how it picks local ports,
and how it cleans up. You do not need any of it to use berth.

## Processes

berth never talks the SSH protocol itself. It runs the OpenSSH client and uses
its connection sharing feature (`ControlMaster`, see `ssh_config(5)`) to add a
watcher session and port forwards to the connection you open.

```
 your terminal
      │
    berth ── ssh <your arguments> ──────────► becomes a client of the master
      │       -o ControlMaster=auto                    │
      │       -o ControlPath=<private socket>          ▼
      │       -o ControlPersist=…           ┌──────────────────┐   one SSH    ┌──────┐
      │                                     │    ssh master    │═════════════▶│ sshd │
      │                                     │   (background)   │  connection  └──┬───┘
      │                                     └──────────────────┘                 ├── your shell or command
      ├── ssh -S <socket> -T berth 'sh -s berth-watcher' ────────────────────────├── watcher script
      └── ssh -S <socket> -O forward -L localhost:3000:127.0.0.1:3000
```

1. berth creates a private directory (mode 0700) in the temporary directory and
   runs `ssh` with your arguments, preceded by options that turn on connection
   sharing through a socket in that directory. ssh uses the first value it gets
   for an option, so these take precedence over your configuration. You
   authenticate as usual: password prompts, host key questions and the like
   work exactly as with plain ssh.
2. After login, `ControlPersist` moves the connection to a background master
   process, and your ssh process continues as a client of it. This is why your
   session ends as soon as you log out, even while other sessions use the
   connection.
3. berth waits until the control socket answers `ssh -O check`, then starts a
   second session, `sh -s berth-watcher`, and sends the watcher script on its
   standard input.
4. As the script reports listening ports, berth adds and removes forwards with
   `ssh -O forward` and `ssh -O cancel`. These helper commands run with
   `-F /dev/null`, because nothing in your configuration is needed to talk to
   the master.
5. When your ssh process exits, berth closes the script's input and sends
   `ssh -O exit`, which closes the connection and all forwards, and returns
   ssh's exit status.

With `-N`, ssh moves to the background right after login and there is no
session to wait for. berth then keeps running, prints a message, and stops when
you press Ctrl-C or the connection closes.

Some command lines are passed to ssh unchanged, without port forwarding: those
with `-G`, `-V`, `-Q` (they do not connect), `-O`, `-M` or `-S` (they manage
connection sharing themselves), `-W` and `-s` (they use the connection for
data), `-f` (it puts ssh in the background), and those without a destination.

## The watcher script

The script is [internal/remote/watch.sh](../internal/remote/watch.sh). It is
plain POSIX `sh`, so it runs on any Unix-like server without installing
anything, whatever the login shell is. It is sent through standard input,
because quoting a script through an unknown login shell (bash, zsh, fish, csh)
is fragile.

It picks the first way of listing sockets that works:

| Method | Used on | Owner of each socket |
| --- | --- | --- |
| `/proc/net/tcp` and `/proc/net/tcp6` | Linux | yes |
| `lsof -iTCP -sTCP:LISTEN` | macOS | yes; without root, only your own processes are listed |
| `ss -tln` | Linux without `/proc` | no |
| `netstat -an` | other systems | no |
| `lsof` | systems with none of the above | yes |

Then it repeats the scan at the interval set with `--interval` and prints the
list whenever it changes. The output is line based:

```
@berth hello 1 <uid> <method> <os> <byte order> <host id>
@berth snapshot
<one line per listening socket, as printed by the method>
@berth end
```

Anything before the `hello` line, such as output from shell startup files, is
ignored. If the script cannot work, it prints `@berth error <reason>` and
exits, and berth reports the reason. Parsing happens in berth, not on the
server, which keeps the script short.

The script exits as soon as its standard input reaches end of file. berth keeps
that input open while it runs, so the script ends when berth closes it on exit,
when berth dies (the operating system closes it), and when the connection goes
away.

## Which ports are forwarded

For every snapshot, berth groups the sockets by port and decides per port:

1. Ports in `--exclude` are never forwarded. Neither are the ports of your own
   `-R` forwards, the X11 display ports 6010-6099 if you use `-X` or `-Y`, and
   ports that berth itself uses locally (see "Loops" below).
2. Ports in `--include` are forwarded.
3. Otherwise the scope decides:
   - `mine`: a socket on the port belongs to the user you log in as.
   - `new`: the port was not listening in the first snapshot. A port that closes
     and opens again counts as new.
   - `all`: always. `none`: never.

`auto` becomes `mine` when the method reports socket owners and you are not
root, and `new` otherwise.

## Choosing addresses and ports

On the server, berth connects to the address the service listens on: 127.0.0.1
for a service on an IPv4 wildcard or loopback address, ::1 for one on IPv6, and
the exact address for a service bound to a specific interface.

On your machine, berth uses the same port number if it is free: nothing accepts
connections on it at 127.0.0.1 or ::1, and it can be bound on both. The first
check matters on macOS, where a socket bound to 127.0.0.1 can share its port
with a socket bound to the wildcard address; without it, a forward could take
over a local service. Checking both addresses avoids a forward that reaches
different services over IPv4 and IPv6. If the port is not free, berth asks the
system for an unused one, unless you gave `--same-port`.

Forwards always listen on `localhost`, so they are never exposed to your
network, even if `GatewayPorts` is set.

## Lifetime of a forward

When a port stops listening, its forward stays for five more seconds, so that a
server that restarts (as development servers do on every change) keeps its
forward and its port number. After that, berth cancels it.

If a port cannot be forwarded, berth reports it once and tries again only after
the port has closed and opened again.

## Loops

If the server is the local machine, its listening ports include berth's own
forwards. Forwarding those again would create forwards that connect to
themselves. berth prevents this in two ways:

- The script reports a host id: the boot id and network namespace on Linux,
  the boot session UUID on macOS. If it matches the local machine, berth
  forwards nothing and says so. Containers usually have a network namespace of
  their own, so berth forwards their ports as usual.
- In any case, berth ignores remote ports whose numbers it is using locally for
  forwards, and the local ports of your own `-L` and `-D` forwards.

## Cleanup

| What happens | What cleans up |
| --- | --- |
| You log out, or the command ends | berth sends `ssh -O exit`; the master closes the connection with all forwards and the script. |
| berth gets `SIGINT`, `SIGTERM` or `SIGHUP` | berth passes the signal on to ssh (or, with `-N`, stops waiting) and cleans up as above. |
| berth is killed with `SIGKILL` | The script sees the end of its input and exits. Your ssh session keeps working; once it ends, the master exits after 5 seconds without sessions (`ControlPersist=5`). |
| berth with `-N` is killed with `SIGKILL` | The script exits, but the master keeps the connection open, as `ssh -f -N` would, until you end it with `ssh -S <socket> -O exit host` or by killing it. |
| The connection drops | ssh exits as usual; berth removes its temporary directory. |

## Limitations

- Only TCP ports are forwarded.
- The OpenSSH client must support connection sharing, so native Windows is not
  supported; WSL is.
- Messages printed on the terminal can disturb full-screen programs; use
  `--notify desktop` or `--quiet`.
- Servers that forbid TCP forwarding accept the forwards but refuse the
  connections through them.
