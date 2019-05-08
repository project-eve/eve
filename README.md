# EVE is Edge Virtualization Engine
[![CircleCI](https://circleci.com/gh/zededa/eve.svg?style=svg)](https://circleci.com/gh/zededa/eve)
[![Goreport](https://goreportcard.com/badge/github.com/zededa/eve)](https://goreportcard.com/report/github.com/zededa/eve)
[![Godoc](https://godoc.org/github.com/zededa/eve/pkg/pillar?status.svg)](https://godoc.org/github.com/zededa/eve/pkg/pillar)

EVE aims to develop an open, agnostic and standardized architecture unifying the approach to developing and orchestrating cloud-native applications across the enterprise on-premises edge. It offers users new levels of control through hardware-assisted virtualization of on-prem edge devices. Once installed, EVE has direct access to and control of underlying resources and provides standard APIs that allow more efficient use of resources and can effectively partition hardware to increase workload consolidation and application multi-tenancy.

EVE supports both ARM and Intel architectures and requires hardware-assisted virtualization. While EVE can run on a board as small as a $20 Orange Pi, the sweet spot for its deployment are IoT Gateways and Industrial PCs.

To get its job done, EVE leverages a lot of great open source projects: [Xen Project](https://xenproject.org/), [Linuxkit](https://github.com/linuxkit/linuxkit) and [Alpine Linux](https://alpinelinux.org/) just to name a few. All of that functionality is being orchestrated by the Go microservices available under [pkg/pillar](pkg/pillar). Why pillar? Well, because pillar is the kind of a monolith we need to break out into true, individual microservices under [pkg/](pkg/).

# How to use

You will need qemu 3.x+ (https://www.qemu.org/), Docker (https://www.docker.com)
and go 1.12+ (https://golang.org) installed in your system.

Note, that since Linuxkit and manifest-tool are evolving pretty rapidly, we're
vendoring those under build-tools/src. This means you don't have to have them
locally installed, but it also means your first build time will be much longer.

If you're on MacOS the following steps should get you all the dependencies:

  0. Get Go:

  ```
  https://golang.org/dl/
  ```
  1. Get Docker:

  ```
  https://store.docker.com/editions/community/docker-ce-desktop-mac
  ```
  2. Make sure brew is installed:

  ```
  https://brew.sh/
  ```
  3. Brew install qemu.

  ```
  $ brew install qemu
  ```

Make sure that Docker is up and running on your system. On MacOS just start a docker Application, on Linux make sure docker service is running. Regardless of how you start Docker you can make sure that it is ready for you by running the following command and making sure that it returns both a version of the client AND a version of the server:

```
docker version
```

EVE requires beeing built in Git repository (the tools keep looking up git commit IDs). The easiest way is to clone EVE repository from GitHub:
```
git clone https://github.com/zededa/eve.git
cd eve
```

Build both the build-tools as well as the live image in the source directory:

```
make build-tools
make live
```
This will download the relevant dockers from docker hub and create a bootable
image 'dist/<ARCH>/live.img'.

Please note that not all containers will be fetched from the docker
hub. mkimage-raw-efi in particular will be built.

Also, keep in mind that since the initial build fetches a LOT of bits
over the network it may occasionally time out and fail. Typically
re-running make fixes the issue. If it doesn't you can attempt a local
build of all the required EVE packages first by running:

```
make pkgs
```

Finally run the resulting image by typing `make run`. This will launch
qemu with some default assumptions.

Once the image boots you can interact with it either by using the console
(right there in the terminal window from which make run was executed).
Your shell in the console is connected to the 'host' os. Everything
interesting is actually happening in the pillar container. Use
`eve enter` command to enter it (or if you're comfortable with ctr CLI
from containerd - use that instead).

Once in a container you can run the usual xl commands to start VMs and
interact with Xen.

# How to use on an ARM board

While running everything on your laptop with qemu could be fun, nothing
beats real hardware. The most cost-effective option, not surprisingly,
is ARM. We recommend using HiKey board (http://www.lenovator.com/product/90.html).
Once you acquire the board you will need to build an installer image by running
(note that if you're building it on an ARM server you can drop ZARCH=arm64 part):
```
make ZARCH=arm64 installer
```
and then flashing it onto an SD card. For example, here's how you can do the
flashing on Mac OS X (where XXX is the name of your SD card as shown by
diskutil list):
```
diskutil list
diskutil umountDisk /dev/rdiskXXX
sudo dd if=dist/arm64/installer.raw of=/dev/rdiskXXX bs=1m
diskutil eject /dev/rdiskXXX
```

Since by default HiKey is using WiFi for all its networking, you will also
have to provide SSID and password for your WiFi network. On Mac OS X you
can simply re-insert SD card and edit wpa_supplicant.conf that will appear 
on volume called EVE.


At this point you have everything you need to permanently install onto
HiKey's internal flash. This, of course, will mean that if you have anything
else installed there (like a Debian or Android OS) it will be replaced so
make sure to make a backup if you nee to.

Additionally, our installer will try to configure an entry point to the
initial boot sequence via GRUB. Since the only reliable way to do so is
by replacing a file called fastboot.efi in the system boot partition you
need to make sure that you have fastboot.efi present there (since if itsn't
there installer will refuse to proceed). The easiest way to check for
all that is to invoke an EFI shell on HiKey. Here's how: put the SD card 
into the KiKey, connect HiKey to your serial port, start screen, poweron
HiKey and immediately start pressing <ESC> key to trigger EFI shell:
```
screen /dev/tty.usbserial-* 115200

[1] fastboot
[2] boot from eMMC
[3] boot from SD card
[4] Shell
[5] Boot Manager
Start: 4
.....
Press ESC in 4 seconds to skip startup.nsh or any other key to continue.

Shell> ls fs2:\EFI\BOOT\fastboot.efi
Shell> setsize 1 fs2:\EFI\BOOT\fastboot.efi
```

NOTE: you only need to execute the last (setsize) command if, for whatever
reason, the previous command doesn't show fastboot.efi present on your
system. Once you've either verified that there's an existing fastboot.efi
(or created a dummy one via the setsize command) you can proceed with
the rest of the installation from the same EFI shell by executing:
```
Shell> fs0:\EFI\BOOT\BOOTX64.EFI
```

You will see an installation sequence scroll on screen and the output
that indicates a successful install will look like this:
```
[   85.717414]  mmcblk0: p1 p2 p3 p4 p5 p6 p7 p8 p11
[   87.420407]  mmcblk0: p1 p2 p3 p4 p5 p6 p7 p8 p11 p12
[  118.754353]  mmcblk0: p1 p2 p3 p4 p5 p6 p7 p8 p11 p12 p13
[  119.801805]  mmcblk0: p1 p2 p3 p4 p5 p6 p7 p8 p11 p12 p13 p14
[  120.992048]  mmcblk0: p1 p2 p3 p4 p5 p6 p7 p8 p11 p12 p13 p14 p19
[  127.191119] reboot: Power down
(XEN) Hardware Dom0 halted: halting machine
```

At this point you should remove your SD card from HiKey's slot and reboot
the board. If everything went as planned you will boot right into the running
system. One thing that you will notice is that a successful installation sequence
made a backup copy of your existing fastboot.efi under the fastboot.efi.XXX name.
This allows you to restore your HiKey to a pristine state without going through
a full fledged re-flashing sequence.

Alternatively, if you're not quite ready to commit to replace your current OS
on the HiKey, you can try running from the SD card. For that you will have to
put a live system on the SD card, not the installer. Here's how you can do that
on Mac OS X:
```
vi conf/wpa_supplicant.conf
  # put your WIFI passwords in and/or add your own networks
make ZARCH=arm64 MEDIA_SIZE=8192 live
sudo dd if=dist/arm64/live.raw of=/dev/rdiskXXX bs=1m
```

Then you can boot into a live system from triggering UEFI shell like shown
above and executing exactly the same boot command:
```
Shell> fs0:\EFI\BOOT\BOOTX64.EFI
```

A quick note on linuxkit: you may be wondering why do we have a container-based
architecture for a Xen-centric environment. First of all, OCI containers
are a key type of a workload for our platform. Which means having
OCI environment to run them is a key requirement. We do plan to run them
via Stage 1 Xen (https://github.com/rkt/stage1-xen) down the road, but 
while that isn't integrated fully we will be simply relying on containerd.
In addition to that, while we plan to build a fully disagregated system 
(with even device drivers running in their separate domains) right now
we are just getting started and having containers as a first step towards
full disagreagation seems like a very convenient stepping stone. 

Let us know what you think by filing GitHub issues, and feel free to 
send us pull requests if something doesn't quite work.
