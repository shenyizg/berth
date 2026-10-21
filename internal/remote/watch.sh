# berth port watcher, protocol 1.
#
# berth runs this script on the remote host as "sh -s berth-watcher", in a
# session of the existing SSH connection. It lists the TCP sockets in the
# LISTEN state at a fixed interval and prints the list whenever it changes:
#
#   @berth hello <protocol> <uid> <method> <os> <byte order> <host id>
#   @berth snapshot
#   <one line per listening socket, in the format of <method>>
#   @berth end
#
# The script exits when its standard input is closed, which happens when berth
# disconnects or goes away. It is a single brace group so that the shell has
# read all of it before running any of it.
{
interval=@INTERVAL@
method=@METHOD@
procnet=@PROCNET@

os=$(uname -s 2>/dev/null || echo -)

if [ "$method" = auto ]; then
	if [ -r "$procnet/tcp" ]; then
		method=proc
	elif [ "$os" = Darwin ] && command -v lsof >/dev/null 2>&1; then
		# Unlike netstat, lsof tells who owns each socket.
		method=lsof
	elif command -v ss >/dev/null 2>&1; then
		method=ss
	elif command -v netstat >/dev/null 2>&1; then
		method=netstat
	elif command -v lsof >/dev/null 2>&1; then
		method=lsof
	else
		echo "@berth error cannot list listening sockets: need /proc/net/tcp, ss, netstat or lsof"
		exit 1
	fi
fi

scan() {
	case $method in
	proc) cat "$procnet/tcp" "$procnet/tcp6" 2>/dev/null | awk '$4 == "0A" { print $2, $8 }' ;;
	ss) ss -tln 2>/dev/null ;;
	netstat) netstat -an 2>/dev/null | awk '$1 ~ /^tcp/ && $NF == "LISTEN" { print $1, $4 }' ;;
	lsof) lsof -nP -iTCP -sTCP:LISTEN -Fptun 2>/dev/null ;;
	esac
}

# /proc/net/tcp prints addresses in the host's byte order.
case $(printf '\001\000' | od -An -tu2 2>/dev/null | tr -d ' ') in
1) order=le ;;
256) order=be ;;
*) order=- ;;
esac

# The host id tells berth whether it is connected to its own machine: on
# Linux, the boot id and network namespace; on macOS, the boot session.
hostid=$( { cat /proc/sys/kernel/random/boot_id; readlink /proc/self/ns/net; sysctl -n kern.bootsessionuuid; } 2>/dev/null | tr -d ' \n')

echo "@berth hello 1 $(id -u 2>/dev/null || echo -) $method $os $order ${hostid:--}"

# Stop when berth closes our standard input. A command started with & gets
# /dev/null as its standard input unless that is redirected explicitly, so the
# reader takes it from a copy on fd 3. (fd 3 stays open here: closing it
# before the loop below redirects its input makes bash 3.2 lose the copy.)
main=$$
exec 3<&0
{ cat <&3 >/dev/null; kill "$main" 2>/dev/null; } &

last=
first=yes
while :; do
	now=$(scan)
	if [ -n "$first" ] || [ "$now" != "$last" ]; then
		printf '@berth snapshot\n%s\n@berth end\n' "$now"
		last=$now
		first=
	fi
	sleep "$interval"
done </dev/null
}
