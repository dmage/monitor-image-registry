#!/bin/sh -eu

: ${OUTDIR:=/usr/share/monitor-image-registry/dir-image}

mkdir -p "$OUTDIR"

# Building an entrypoint for the container
cc -static -o hello ./hello.c

# Creating the layer
tar --owner=0 --group=0 -cf layer.tar hello
LAYER="$(sha256sum ./layer.tar)"
LAYER="${LAYER%% *}"
gzip ./layer.tar
BLOB="$(sha256sum ./layer.tar.gz)"
BLOB="${BLOB%% *}"
mv ./layer.tar.gz "$OUTDIR/$BLOB"

# Creating the image config
NOW="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
printf '{"created":"%s","author":"","architecture":"amd64","os":"linux"' "$NOW" >./config.json
printf ',"config":{"Cmd":["/hello"]}' >>./config.json
printf ',"rootfs":{"diff_ids":["sha256:%s"],"type":"layers"}' "$LAYER" >>./config.json
printf ',"history":[{"created":"%s","created_by":"/bin/true"}]}' "$NOW" >>./config.json
CONFIG="$(sha256sum ./config.json)"
CONFIG="${CONFIG%% *}"
mv ./config.json "$OUTDIR/$CONFIG"

# Creating the manifest
printf '{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json"' >./manifest.json
printf ',"config":{"mediaType":"application/vnd.docker.container.image.v1+json","size":%d,"digest":"sha256:%s"}' "$(wc -c <"$OUTDIR/$CONFIG")" "$CONFIG" >>./manifest.json
printf ',"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","size":%d,"digest":"sha256:%s"}]}' "$(wc -c <"$OUTDIR/$BLOB")" "$BLOB" >>./manifest.json
mv ./manifest.json "$OUTDIR/manifest.json"
