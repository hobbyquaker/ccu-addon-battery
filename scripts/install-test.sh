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
#   3. the x86_64 package installs again as an update (exit 0): the service stopped and started.
#
set -e
cd "$(dirname "$0")/.."
VERSION=${1:?usage: scripts/install-test.sh <version>}
IMAGE=${BUSYBOX_IMAGE:-busybox:1.36.1}
[ "$(uname -m)" = x86_64 ] || { echo "install-test: needs an x86_64 host" >&2; exit 1; }
for arch in x86_64 aarch64 armv7l; do
    [ -f "dist/battery-$arch-$VERSION.tar.gz" ] || { echo "install-test: dist/battery-$arch-$VERSION.tar.gz is missing" >&2; exit 1; }
done

docker run --rm -i -v "$PWD/dist:/dist:ro" -e VERSION="$VERSION" "$IMAGE" sh -s <<'EOF'
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
mkdir -p /usr/local/tmp /usr/local/etc/config
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

: > /tmp/rc-calls
install x86_64; code=$?
[ $code -eq 0 ] || { cat /tmp/out; fail "update: exit $code, want 0"; }
[ "$(cat /tmp/rc-calls | tr '\n' ' ')" = "stop " ] || fail "update: the old service got: $(cat /tmp/rc-calls)"
grep -q "^VERSION_ADDON=$VERSION$" /usr/local/addons/battery/versions || fail "update: versions"
echo "ok   x86_64 installs as an update (exit 0)"
EOF
