# Device API

This is the "Device API", for communications between an edge device and a controller.

See [https://www.lfedge.org/projects/eve/](https://www.lfedge.org/projects/eve/)

This directory defines only the API itself. It is in two parts:

* documentation in the file [API.md](./API.md) for the protocol
* message definitions as [protobufs](https://developers.google.com/protocol-buffers/) in subdirectories to this directory

To use the protobufs, you need to compile them into the target language of your choice, such as Go, Python or Node.
The actual compiled language-specific libraries are in the [sdk/](../sdk/) in the root directory of this repository, and are compiled via the
command `make sdk` in the root of this repository.
