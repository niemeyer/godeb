# In-flight deb packages of Go

Introduction
------------

For details of how this tool works and context for why it was built,
please refer to the following blog post:

  * [In-flight deb packages of Go](http://blog.labix.org/2013/06/15/in-flight-deb-packages-of-go)


Installation and usage
----------------------

If you already have a Go toolset avaliable, run:

    go get gopkg.in/niemeyer/godeb.v1/cmd/godeb

Otherwise, there are pre-built binaries available for the
[amd64](https://godeb.s3.amazonaws.com/godeb-amd64.tar.gz) and
[386](https://godeb.s3.amazonaws.com/godeb-386.tar.gz).
architectures


License
-------

The godeb code is licensed under the LGPL with an exception that allows it to be linked statically. Please see the LICENSE file for details.
