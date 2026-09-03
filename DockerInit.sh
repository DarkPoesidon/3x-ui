#!/bin/sh
case $1 in
    amd64)
        ARCH="64"
        FNAME="amd64"
        ;;
    i386)
        ARCH="32"
        FNAME="i386"
        ;;
    armv8 | arm64 | aarch64)
        ARCH="arm64-v8a"
        FNAME="arm64"
        ;;
    armv7 | arm | arm32)
        ARCH="arm32-v7a"
        FNAME="arm32"
        ;;
    armv6)
        ARCH="arm32-v6"
        FNAME="armv6"
        ;;
    *)
        ARCH="64"
        FNAME="amd64"
        ;;
esac
# The AnyTLS sidecar must come from a build carrying this panel's multi-user
# management API; upstream ssrlive/anytls-rs has no such API and would leave
# every anytls inbound unusable. Default to an anytls-rs beside this panel under
# the same GitHub owner, taken from the module path so a rebranded fork points
# at its own org without editing anything.
ANYTLS_OWNER=$(sed -n 's#^module github.com/\([^/]*\)/.*#\1#p' go.mod | head -n 1)
ANYTLS_REPO="${ANYTLS_REPO:-${ANYTLS_OWNER}/anytls-rs}"
ANYTLS_VER=$(curl -sfL "https://api.github.com/repos/${ANYTLS_REPO}/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
if [ -z "$ANYTLS_VER" ]; then
    echo "DockerInit: no release found at github.com/${ANYTLS_REPO}." >&2
    echo "DockerInit: publish the patched anytls-rs there, or set ANYTLS_REPO to the repo that has it." >&2
    exit 1
fi
MTG_MULTI_VER=$(curl -sfL "https://api.github.com/repos/mhsanaei/mtg-multi/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
if [ -z "$MTG_MULTI_VER" ]; then
    echo "DockerInit: could not resolve the latest mtg-multi release tag" >&2
    exit 1
fi
mkdir -p build/bin
cd build/bin
curl -sfLRO "https://github.com/XTLS/Xray-core/releases/download/v26.7.28/Xray-linux-${ARCH}.zip"
unzip "Xray-linux-${ARCH}.zip"
rm -f "Xray-linux-${ARCH}.zip" geoip.dat geosite.dat
mv xray "xray-linux-${FNAME}"
# mtg-multi (MTProto sidecar) ships prebuilt release binaries for every target
# we package, so download and unpack the matching one instead of compiling.
case $FNAME in
    i386) MTGARCH="386" ;;
    arm32) MTGARCH="armv7" ;;
    *) MTGARCH="$FNAME" ;;
esac
MTG_PKG="mtg-multi-${MTG_MULTI_VER#v}-linux-${MTGARCH}"
curl -sfLRO "https://github.com/mhsanaei/mtg-multi/releases/download/${MTG_MULTI_VER}/${MTG_PKG}.tar.gz"
tar -xzf "${MTG_PKG}.tar.gz"
mv "${MTG_PKG}/mtg-multi" "mtg-linux-${FNAME}"
rm -rf "${MTG_PKG}" "${MTG_PKG}.tar.gz"
chmod +x "mtg-linux-${FNAME}"
# anytls-server ships one zip per Rust target triple; pick the static musl build
# matching this image's architecture.
case $FNAME in
    amd64) ANYTLS_TARGET="x86_64-unknown-linux-musl" ;;
    arm64) ANYTLS_TARGET="aarch64-unknown-linux-musl" ;;
    armv7|arm32) ANYTLS_TARGET="armv7-unknown-linux-musleabihf" ;;
    armv6) ANYTLS_TARGET="arm-unknown-linux-musleabihf" ;;
    i386) ANYTLS_TARGET="i686-unknown-linux-musl" ;;
    *) ANYTLS_TARGET="" ;;
esac
if [ -n "$ANYTLS_TARGET" ]; then
    curl -sfLRO "https://github.com/${ANYTLS_REPO}/releases/download/${ANYTLS_VER}/anytls-${ANYTLS_TARGET}.zip"
    unzip -o -j "anytls-${ANYTLS_TARGET}.zip" anytls-server -d .
    mv anytls-server "anytls-server-linux-${FNAME}"
    rm -f "anytls-${ANYTLS_TARGET}.zip" anytls-client
    chmod +x "anytls-server-linux-${FNAME}"
fi
curl -sfLRO https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat
curl -sfLRO https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat
curl -sfLRo geoip_IR.dat https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geoip.dat
curl -sfLRo geosite_IR.dat https://github.com/chocolate4u/Iran-v2ray-rules/releases/latest/download/geosite.dat
curl -sfLRo geoip_RU.dat https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geoip.dat
curl -sfLRo geosite_RU.dat https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geosite.dat
cd ../../
