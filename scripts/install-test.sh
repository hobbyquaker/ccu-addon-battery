#!/bin/sh
#
#   The install test: the packages in dist/ installed by their update_script in a busybox
#   container (an x86_64 host), as OpenCCU's installer runs it.
#
#     scripts/install-test.sh <version>       # after VERSION=<version> scripts/build.sh
#
#   1. the x86_64 package installs fresh (exit 10);
#   2. the aarch64 and the armv7l package are refused (exit 13) and leave the installed version
#      as it was: its service not stopped, its files unchanged;
#   3. the x86_64 package installs again as an update (exit 0): a service that was stopped is
#      stopped and stays stopped, the kit's release cache is gone;
#   4. an update of a running service (the real daemon) starts it again;
#   5. the CCU3 firmware (S00InstallAddon): its installer's chroot replayed (a copy of the root,
#      /proc, /dev, /sys and /usr/local bound in): a running service is started again in the real
#      root, not in the chroot; a stopped one (the install during the shutdown) is not started;
#   6. the update_script's snippets are the kit's (ui/node_modules/ccu-addon-kit, after the build).
#
#   The container runs privileged: the chroot replay binds directories.
set -e
cd "$(dirname "$0")/.."
VERSION=${1:?usage: scripts/install-test.sh <version>}
IMAGE=${BUSYBOX_IMAGE:-busybox:1.36.1}
[ "$(uname -m)" = x86_64 ] || { echo "install-test: needs an x86_64 host" >&2; exit 1; }
for arch in x86_64 aarch64 armv7l; do
    [ -f "dist/battery-$arch-$VERSION.tar.gz" ] || { echo "install-test: dist/battery-$arch-$VERSION.tar.gz is missing" >&2; exit 1; }
done

docker run --rm -i --privileged -v "$PWD/dist:/dist:ro" -e VERSION="$VERSION" "$IMAGE" sh -s <<'EOF'
set -u
fail() { echo "FAIL: $*"; exit 1; }
install() {
    rm -rf /usr/local/tmp/pkg && mkdir -p /usr/local/tmp/pkg && cd /usr/local/tmp/pkg
    tar -xzf "/dist/battery-$1-$VERSION.tar.gz" || fail "unpack $1"
    ./update_script HM-RASPBERRYMATIC > /tmp/out 2>&1
    code=$?
    cd /
    rm -rf /usr/local/tmp/pkg
    return $code
}
mkdir -p /usr/local/tmp /usr/local/etc/config /var/log
# the pid of the running daemon (any process whose arguments name it), empty when none runs
running() {
    for d in /proc/[0-9]*; do
        tr '\0' '\n' < $d/cmdline 2>/dev/null | grep -qx /usr/local/addons/battery/bin/battery && { echo ${d#/proc/}; return; }
    done
}
install x86_64; code=$?
[ $code -eq 10 ] || { cat /tmp/out; fail "fresh install: exit $code, want 10"; }
[ -x /usr/local/addons/battery/bin/battery ] || fail "fresh install: no bin/battery"
echo "ok   x86_64 installs fresh (exit 10)"

# the installed version's service: a stand-in that records what it is asked
rm -f /usr/local/etc/config/rc.d/battery
printf '#!/bin/sh\necho "$1" >> /tmp/rc-calls\n' > /usr/local/etc/config/rc.d/battery
chmod 755 /usr/local/etc/config/rc.d/battery
before=$(cd /usr/local/addons/battery && find . -type f -exec md5sum {} + | sort)

for arch in aarch64 armv7l; do
    : > /tmp/rc-calls
    install $arch; code=$?
    [ $code -eq 13 ] || { cat /tmp/out; fail "$arch: exit $code, want 13"; }
    grep -q "this system is x86_64: please download battery-x86_64-$VERSION.tar.gz" /tmp/out || { cat /tmp/out; fail "$arch: the message"; }
    grep -q "Dieses Paket ist für $arch" /tmp/out || { cat /tmp/out; fail "$arch: the German message"; }
    [ ! -s /tmp/rc-calls ] || fail "$arch: the installed service was asked: $(cat /tmp/rc-calls)"
    after=$(cd /usr/local/addons/battery && find . -type f -exec md5sum {} + | sort)
    [ "$before" = "$after" ] || fail "$arch: the installed files changed"
    echo "ok   $arch refused (exit 13), the installed version untouched"
done

# 3. the stand-in says stopped (status exit 3): stopped, not started
printf '#!/bin/sh\necho "$1" >> /tmp/rc-calls\n[ "$1" = status ] && exit 3\nexit 0\n' > /usr/local/etc/config/rc.d/battery
echo '{"tag_name":"v0.0.1"}' > /tmp/kit-battery-release.json
: > /tmp/rc-calls
install x86_64; code=$?
[ $code -eq 0 ] || { cat /tmp/out; fail "update: exit $code, want 0"; }
[ "$(cat /tmp/rc-calls | tr '\n' ' ')" = "status stop " ] || fail "update: the old service got: $(cat /tmp/rc-calls)"
grep -q "^VERSION_ADDON=$VERSION$" /usr/local/addons/battery/versions || fail "update: versions"
[ -z "$(running)" ] || fail "update: a stopped service was started"
[ ! -e /tmp/kit-battery-release.json ] || fail "update: the kit's release cache is still there"
echo "ok   x86_64 installs as an update (exit 0), a stopped service stays stopped"

# 4. the real daemon running: started again after the update
RC=/usr/local/etc/config/rc.d/battery
$RC start > /tmp/out 2>&1 || { cat /tmp/out; fail "start the installed service"; }
old=$(running); [ -n "$old" ] || fail "the service does not run before the update"
install x86_64; code=$?
[ $code -eq 0 ] || { cat /tmp/out; fail "update (running): exit $code, want 0"; }
new=$(running)
[ -n "$new" ] || { cat /tmp/out; fail "update (running): the service is not running after the update"; }
[ "$new" != "$old" ] || fail "update (running): still the old process $old"
[ "$(readlink /proc/$new/root)" = / ] || fail "update (running): runs in $(readlink /proc/$new/root)"
echo "ok   an update of a running service starts it again"

# 5. the CCU3 firmware: S00InstallAddon and its installer's chroot
touch /etc/init.d/S00InstallAddon 2>/dev/null || { mkdir -p /etc/init.d && touch /etc/init.d/S00InstallAddon; }
install_ccu3() {
    rm -f /usr/local/tmp/new_addon.tar.gz
    dir=$(mktemp -d -p /usr/local/tmp)
    tar -C "$dir" -xzf "/dist/battery-x86_64-$VERSION.tar.gz" || fail "unpack (ccu3)"
    for d in bin sbin lib lib64 etc usr; do
        [ -e /$d ] || continue
        if [ "$d" = usr ]; then
            mkdir -p "$dir/usr"
            for u in /usr/*; do [ "$u" = /usr/local ] || cp -a "$u" "$dir/usr/"; done
        else
            cp -a /$d "$dir/"
        fi
    done
    for m in proc sys dev usr/local; do mkdir -p "$dir/$m"; mount -o bind /$m "$dir/$m" || fail "bind $m"; done
    chroot "$dir" /bin/sh -c 'cd / && ./update_script HM-RASPBERRYMATIC' > /tmp/out 2>&1
    code=$?
    for m in usr/local dev sys proc; do umount "$dir/$m" 2>/dev/null || umount -l "$dir/$m"; done
    grep -q " $dir/" /proc/mounts || rm -rf "$dir"
    return $code
}
old=$(running); [ -n "$old" ] || fail "ccu3: the service does not run before the update"
install_ccu3; code=$?
[ $code -eq 0 ] || { cat /tmp/out; fail "ccu3 (running): exit $code, want 0"; }
new=$(running)
[ -n "$new" ] || { cat /tmp/out; fail "ccu3 (running): the service is not running after the live update"; }
[ "$new" != "$old" ] || fail "ccu3 (running): still the old process $old"
[ "$(readlink /proc/$new/root)" = / ] || fail "ccu3 (running): started inside the chroot ($(readlink /proc/$new/root))"
echo "ok   ccu3, a live update from the installer's chroot: started again, in the real root"

$RC stop > /tmp/out 2>&1 || { cat /tmp/out; fail "stop"; }
install_ccu3; code=$?
[ $code -eq 0 ] || { cat /tmp/out; fail "ccu3 (stopped): exit $code, want 0"; }
[ -z "$(running)" ] || fail "ccu3 (stopped): started from the chroot (the boot starts it)"
echo "ok   ccu3, the install during the shutdown (service stopped): nothing started"
rm -f /etc/init.d/S00InstallAddon
EOF

# 6. the snippets the kit's update_script-reference marks are the same in battery's update_script
KIT_REF=ui/node_modules/ccu-addon-kit/classic/sh/update_script-reference
[ -f $KIT_REF ] || { echo "install-test: $KIT_REF is missing (scripts/build.sh runs npm ci)" >&2; exit 1; }
snippets() { awk '/^# --- ccu-addon-kit: /{on=1} on{print} /^# --- end ---$/{on=0}' "$1"; }
[ -n "$(snippets $KIT_REF)" ] || { echo "FAIL: no snippets in the kit's reference" >&2; exit 1; }
if [ "$(snippets $KIT_REF)" != "$(snippets addon/update_script)" ]; then
    echo "FAIL: addon/update_script's ccu-addon-kit snippets differ from the kit's reference:" >&2
    snippets $KIT_REF > /tmp/kit-snippets.$$; snippets addon/update_script | diff /tmp/kit-snippets.$$ - >&2; rm -f /tmp/kit-snippets.$$
    exit 1
fi
echo "ok   update_script carries the kit's snippets"
