# docker-regclient

A small utility for listing and deleting images in a Docker registry.
Can be invoked from cron to periodically cleanup images from a Docker registry.

## Usage
Imagine that you have a Docker registry containing images in two repositories
webserver:devbuild-5
webserver:devbuild-4
webserver:rc3
webserver:devbuild-3
webserver:devbuild-2
webserver:devbuild-1
webserver:rc2
.....

backend-server:dev-56
backend-server:dev-55
backend-server:dev-54
backend-server:dev-53
.....

You could use the following command line to delete all but the latest three images from the `webserver` and `backend-server` repositories that have a tag containing the string `dev`.

```
docker-regclient -url https://my.docker.registry \
    images \
    --repo webserver \
    --repo backend-server \
    --exclude-latest 3 \
    --tag-contains dev \
    --delete \
    --yes
```

This is useful when you have some CI system that automatically builds and pushes new Docker images into your registry and you only want to keep the latest n images.

## Building locally
* Install Go
* go get github.com/loginoff/docker-regclient

## Disclaimer
Use at your own peril. In case you manage to somehow destroy all data in your registry using this code, the author can in no way be held responsible.
