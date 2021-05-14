# otto
OCI images to ostree commits

## About
This is a small proof of concept that demonstrates updating of OSTree
repository via a commit that is transported via an OCI Image Archive.

The OSTree commit for a specific repository and branch is transported
via a OSTree repository that is inside an [OCI Image Archive][oci-spec].
This project, `otto`, provides a web server that serves an OSTree
repository via HTTP at `/ostree/repo` but also implements a minimal
container registry that can be used to push container images to via 
the docker registry [API v2][reg-api]. When the image manifest is
being pushed, in the last step of uploading a new OCI Image Archive,
the annotations are checked and they **must** contain the following
key and value pairs:
  - `org.osbuild.ostree.repo`: location of the OSTree repo inside
    the image archive.
  - `org.osbuild.ostree.ref`: OSTree reference of the commit that
    should be imported and branch it should be imported to
  - `org.osbuild.ostree.layer`: identifer (index or digest) of the
    layer that contains the OSTree repo
If those are not provided, the image push will not be accepted.

On a successful push of a new OSTree Image Archive with a contained
commit, the layer is then unpacked, and pulled into the OSTree repo.
Additionally the OSTree summary is updated.

[oci-spec]: https://github.com/opencontainers/image-spec
[reg-api]: https://docs.docker.com/registry/spec/api/

## Getting started
A Dockerfile and a simple Makefile to generate the image, start and
stop the container is included:

```
make build  # build otto, also generates TLS certificates
make start  # start the container, mounts $pwd at /source
make stop   # stop the container
```

The `skopeo` binary is installed in the container so that it is easy
to test pushing an existing container:

```
docker exec -it otto /bin/bash
cd /source
skopeo copy oci-archive:container.tar docker://localhost:3000/test
```