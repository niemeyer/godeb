#!/bin/bash
# (c) Michael Gebetsroither <mgebetsroither@mgit.at>
# License: Apache 2

set -e

DEBUG=${DEBUG:-false}
ARCHS=${1:-"386 amd64 arm-5 arm-6 arm-7 arm64 s390x ppc64le"}
echo "Building godeb for $ARCHS"

for i in $ARCHS; do
    (
    cd cmd/godeb
    rm -f godeb
    case "$i" in
        arm-*)
            GOARCH="${i%%-*}"
            GOARM="${i##*-}"
            echo "GOARCH=\"$GOARCH\" GOARM=\"$GOARM\""
            ;;
        *)
            GOARCH="$i"
            echo "GOARCH=\"$GOARCH\""
            ;;
    esac
    $DEBUG && continue
    CGO_ENABLED=0 GOARCH=$GOARCH GOARM=$GOARM go build -tags netgo
    case "$GOARCH" in
        386|amd64) echo "stripping $GOARCH"; strip --strip-all godeb ;;
    esac
    tar czf ../../godeb-${GOARCH}${GOARM}.tar.gz godeb
    )
done
