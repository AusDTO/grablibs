# `grablibs`

This is a hacky little tool that takes a binary in one Docker image, and then finds all dependent shared libaries needed to make it work on a different image.

It does this by creating a temporary container with both source and destination images, it copies the binaries across to the destination, and then keeps running `ldd` on the destination until such time it has tracked down all needed files.

Finally it writes out a gzipped tar-ball to stdout.

## Quickstart

```bash
go get github.com/AusDTO/grablibs/cmd/grablibs
```

(assumes Docker is running)

## Example

The following gets all dependences needed to run `pg_dump` and `pg_restore` on `ubuntu:14.04`:

```bash
grablibs -source postgres:9.6 -dest ubuntu:14.04 -binaries pg_dump,pg_restore > pgutils.tgz
grablibs -source mysql:5.7    -dest ubuntu:14.04 -binaries mysqldump          > mysqlutils.tgz
```

Looking at the result:

```bash
$ tar -ztf pgutils.tgz
./
./pg_dump
./libgnutls-deb0.so.28
./libtasn1.so.6
./libnettle.so.4
./libcrypto.so.1.0.0
./libhogweed.so.2
./libkrb5support.so.0
./pg_restore
./libssl.so.1.0.0
./libgssapi_krb5.so.2
./libsasl2.so.2
./libp11-kit.so.0
./liblber-2.4.so.2
./libkrb5.so.3
./libkeyutils.so.1
./libgmp.so.10
./libk5crypto.so.3
./libpq.so.5
./libldap_r-2.4.so.2
./libffi.so.6
```

To use these:

```bash
tar zxf pgutils.tgz
LD_LIBRARY_PATH=$PWD ./pg_dump
```

## Troubleshooting

Make sure you have the Docker images present (e.g. do a `docker pull`) before attempting - it'll give the containers 5 seconds to launch else assume failure.
