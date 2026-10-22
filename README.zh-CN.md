# berth

[![CI](https://github.com/shenyizg/berth/actions/workflows/ci.yml/badge.svg)](https://github.com/shenyizg/berth/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[English](README.md) | 简体中文

**berth 是带自动端口转发的 `ssh`。** 它持续监视远程主机上开始监听的 TCP
端口，一出现就转发到你的本机。你通过 SSH 启动的服务，马上就能在 `localhost`
上访问。任何终端都能用，不依赖编辑器，也不需要额外配置。

```console
$ berth dev.example.com
me@dev:~$ npm run dev

  VITE v5.4.0  ready in 412 ms

  ➜  Local:   http://localhost:5173/
berth: forwarded remote port 5173 to localhost:5173
```

在自己电脑上打开 <http://localhost:5173>，访问的就是 `dev.example.com` 上的服务。

## 特性

- **直接替代 ssh。** berth 调用你本机的 `ssh` 并原样传入参数，所以
  `~/.ssh/config`、密钥、agent、跳板机以及所有 ssh 选项都照常可用。
- **全自动。** 新开始监听的端口大约一秒内就会被转发；端口关闭后，转发也随之撤销。
- **端口号保持一致。** 远程端口会转发到本机的同一端口号，所以远程程序打印出来的
  URL 可以直接用，包括会把浏览器跳转到 `http://localhost:<端口>` 的登录流程。
  如果本机这个端口被占用，berth 会另选一个空闲端口并告诉你。
- **只转发你的端口。** 默认只转发你登录所用的用户打开的端口，不会把服务器上的
  各种系统服务都转发过来。
- **服务器端零安装。** berth 通过现有连接发送一段简短的 POSIX shell 脚本，
  适用于 Linux、macOS 以及其他类 Unix 系统。
- **一条连接，一次登录。** 所有操作都复用 OpenSSH 的连接共享，只需认证一次。
  转发只监听本机的回环地址。
- **自动清理。** 退出登录时，所有转发和服务器上的脚本都会一并结束，即使 berth
  进程本身被强行杀掉也是如此。

## 安装

使用 Go 1.24 或更新版本：

```sh
go install github.com/shenyizg/berth/cmd/berth@latest
```

`berth` 会被安装到 `~/go/bin`（准确地说是 `$(go env GOPATH)/bin`）。如果之后 shell
提示 `command not found`，就把这个目录加进 `PATH`：在 shell 的启动文件（如
`~/.zshrc` 或 `~/.bashrc`）里加入下面这一行，然后打开一个新终端：

```sh
export PATH="$PATH:$HOME/go/bin"
```

也可以从 [Releases 页面](https://github.com/shenyizg/berth/releases) 下载预编译的
二进制文件，放到 `PATH` 里的某个目录中；或者克隆本仓库后执行 `make build`。

本机需要 OpenSSH 客户端，macOS、Linux、各种 BSD 和 WSL 都可以。（Windows 版
OpenSSH 不支持连接共享。）服务器需要有 `/bin/sh`，并允许 TCP 转发，这也是默认配置。

## 用法

```
berth [berth 选项] [ssh 选项] 目标主机 [命令]
```

凡是用 ssh 的地方都可以换成 berth。berth 自己的选项都是长选项，必须写在最前面；
其余参数原样交给 ssh。

```sh
berth dev.example.com                    # 打开 shell，端口一打开就自动转发
berth -p 2222 -J bastion me@10.0.0.7     # 任何 ssh 选项都可以用
berth dev 'cd app && npm run dev'        # 在命令运行期间转发端口
berth -N dev                             # 不开 shell，只转发端口，Ctrl-C 结束
berth --exclude 5432,6379 dev            # 永远不转发这些端口
berth --include 8888 dev                 # 连接之前就已打开的端口也要转发
```

想让所有交互式连接都使用 berth，可以在 shell 的启动文件里加一个别名：

```sh
alias ssh=berth
```

git、scp、rsync 等程序仍会调用真正的 ssh，不受影响。

### 选项

| 选项 | 说明 |
| --- | --- |
| `--scope MODE` | 转发哪些监听端口：`auto`（默认）、`mine`、`new`、`all` 或 `none`，见下文。 |
| `--include PORTS` | 这些端口一旦处于监听状态就转发，例如 `8888,9000-9010`。 |
| `--exclude PORTS` | 永远不转发这些端口，优先级高于 `--include`。 |
| `--same-port` | 只转发到本机的同号端口；本机端口被占用时直接跳过，不改用其他端口。 |
| `--interval TIME` | 检查服务器监听端口的时间间隔（默认 `1s`）。 |
| `--notify MODE` | 新转发的提示方式：`terminal`（默认）、`desktop` 或 `none`。 |
| `--quiet` | 等同于 `--notify none`。 |
| `--ssh PROGRAM` | 要运行的 ssh 程序（默认取 `$BERTH_SSH`，否则为 `ssh`）。 |
| `--verbose` | 输出 berth 的详细运行信息。 |
| `--version` | 打印版本号。 |
| `-h`, `--help` | 打印选项说明。 |

### 哪些端口会被转发

| 模式 | 转发的端口 |
| --- | --- |
| `mine` | 你登录所用的用户打开的端口，包括连接之前就已在监听的端口。需要服务器能报告每个 socket 的属主，Linux 和 macOS 都可以。 |
| `new` | 连接之后才开始监听的端口。 |
| `all` | 所有监听端口。 |
| `none` | 只转发 `--include` 指定的端口。 |
| `auto` | 服务器能报告 socket 属主、且不是以 root 登录时为 `mine`，否则为 `new`。 |

`--include` 和 `--exclude` 在所有模式下都生效。你自己用 `-R` 建立的远程转发端口，
以及使用 `-X`/`-Y` 时的 X11 显示端口，berth 都不会转发。

### 提示信息

berth 会在终端里报告新建的转发：

```
berth: forwarded remote port 3000 to localhost:3000
berth: forwarded remote port 5000 to localhost:61202 (local port 5000 is not available)
```

这些提示可能出现在全屏程序的界面中间。可以用 `--notify desktop` 改为桌面通知
（macOS，或装有 `notify-send` 的 Linux），也可以用 `--quiet` 关闭。

## 工作原理

1. berth 运行 `ssh` 时开启连接共享（`ControlMaster` 和 `ControlPersist`），
   使用一个私有的控制 socket。你照常登录即可。
2. berth 在同一条连接上再开一个会话，运行一段小的 shell 脚本。这段脚本每秒列出
   一次正在监听的 TCP socket（Linux 上读取 `/proc/net/tcp`，其他系统用 `lsof`、
   `ss` 或 `netstat`），有变化就报告给 berth。
3. 对每个新端口，berth 都在这条连接上添加一个转发，效果相当于执行
   `ssh -O forward -L localhost:PORT:127.0.0.1:PORT`。
4. 会话结束时，berth 关闭连接，脚本随之结束，所有转发也都被移除。

详细说明见 [docs/how-it-works.md](docs/how-it-works.md)。

## 常见问题

**没有任何端口被转发。** 加上 `--verbose` 运行，看看 berth 在服务器上发现了什么。
常见原因有：

- 服务器禁止转发（`sshd_config` 中设置了 `AllowTcpForwarding no`）。这时本地端口
  能打开，但连接会失败。
- 使用 `new` 模式时，连接之前就已在监听的端口不会被转发。可以用 `--include`
  加上它们，或者使用 `--scope all`。
- 你以 root 身份登录，此时 `auto` 等同于 `new`。

**端口出现在了另一个端口号上。** 这个端口在你的电脑上已被占用，所以 berth 另选了
一个空闲端口。如果希望直接跳过这类端口，使用 `--same-port`。

**berth 提示目标主机 "is this machine"。** 你连接的是自己的电脑，或者是与本机共用
网络的容器。它的端口本来就能直接访问，所以 berth 不做转发。

**服务器是 Windows。** 不支持：脚本需要 POSIX shell。

## 安全性

转发只监听本机的回环地址（127.0.0.1 和 ::1），无论 `GatewayPorts` 如何设置，
都不会暴露到局域网。与 `ssh -L` 一样，本机上的其他程序和用户可以连接这些端口。
控制 socket 创建在一个私有的临时目录中。

## 许可证

berth 以 [MIT 许可证](LICENSE) 发布。
