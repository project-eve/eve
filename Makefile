# Copyright (c) 2018 Zededa, Inc.
# SPDX-License-Identifier: Apache-2.0
#
# Run make (with no arguments) to see help on what targets are available

GOVER ?= 1.12.4
PKGBASE=github.com/zededa/eve
GOMODULE=$(PKGBASE)/pkg/pillar
GOTREE=$(CURDIR)/pkg/pillar
PROTO_LANGS?=go python

APIDIRS = $(shell find ./api/* -maxdepth 1 -type d -exec basename {} \;)

PATH := $(CURDIR)/build-tools/bin:$(PATH)

export CGO_ENABLED GOOS GOARCH PATH

# How large to we want the disk to be in Mb
MEDIA_SIZE=8192
IMG_FORMAT=qcow2
ROOTFS_FORMAT=squash

SSH_PORT := 2222

CONF_DIR=conf

USER         = $(shell id -u -n)
GROUP        = $(shell id -g -n)
UID          = $(shell id -u)
GID          = $(shell id -g)

EVE_TREE_TAG = $(shell git describe --abbrev=8 --always --dirty)

HOSTARCH:=$(shell uname -m)
# by default, take the host architecture as the target architecture, but can override with `make ZARCH=foo`
#    assuming that the toolchain supports it, of course...
ZARCH ?= $(HOSTARCH)
# warn if we are cross-compiling and track it
CROSS ?=
ifneq ($(HOSTARCH),$(ZARCH))
CROSS = 1
$(warning "WARNING: We are assembling a $(ZARCH) image on $(HOSTARCH). Things may break.")
endif
# canonicalized names for architecture
ifeq ($(ZARCH),aarch64)
        ZARCH=arm64
endif
ifeq ($(ZARCH),x86_64)
        ZARCH=amd64
endif

QEMU_SYSTEM_arm64:=qemu-system-aarch64
QEMU_SYSTEM_amd64:=qemu-system-x86_64
QEMU_SYSTEM=$(QEMU_SYSTEM_$(ZARCH))

# where we store outputs
DIST=$(CURDIR)/dist/$(ZARCH)

DOCKER_ARCH_TAG=$(ZARCH)

LIVE_IMG=$(DIST)/live
ROOTFS_IMG=$(DIST)/rootfs.img
TARGET_IMG=$(DIST)/target.img
INSTALLER_IMG=$(DIST)/installer
CONFIG_IMG=$(DIST)/config.img

BIOS_IMG=$(DIST)/bios/OVMF.fd
EFI_PART=$(DIST)/bios/EFI

QEMU_OPTS_arm64= -machine virt,gic_version=3 -machine virtualization=true -cpu cortex-a57 -machine type=virt
# -drive file=./bios/flash0.img,format=raw,if=pflash -drive file=./bios/flash1.img,format=raw,if=pflash
# [ -f bios/flash1.img ] || dd if=/dev/zero of=bios/flash1.img bs=1048576 count=64
QEMU_OPTS_amd64= -cpu SandyBridge
QEMU_OPTS_COMMON= -smbios type=1,serial=31415926 -m 4096 -smp 4 -display none -serial mon:stdio -bios $(BIOS_IMG) \
        -rtc base=utc,clock=rt \
        -nic user,id=eth0,net=192.168.1.0/24,dhcpstart=192.168.1.10,hostfwd=tcp::$(SSH_PORT)-:22 \
        -nic user,id=eth1,net=192.168.2.0/24,dhcpstart=192.168.2.10
QEMU_OPTS=$(QEMU_OPTS_COMMON) $(QEMU_OPTS_$(ZARCH))

GOOS=linux
CGO_ENABLED=1
GOBUILDER=eve-build-$(USER)

DOCKER_UNPACK= _() { C=`docker create $$1 fake` ; docker export $$C | tar -xf - $$2 ; docker rm $$C ; } ; _
DOCKER_GO = _() { mkdir -p $(CURDIR)/.go/src/$${3:-dummy} ;\
    docker run -it --rm -u $(USER) -w /go/src/$${3:-dummy} \
    -v $(CURDIR)/.go:/go -v $$2:/go/src/$${3:-dummy} -v $${4:-$(CURDIR)/.go/bin}:/go/bin -v $(CURDIR)/:/eve -v $${HOME}:/home/$(USER) \
    -e GOOS -e GOARCH -e CGO_ENABLED -e BUILD=local $(GOBUILDER) bash --noprofile --norc -c "$$1" ; } ; _

PARSE_PKGS=$(if $(strip $(EVE_HASH)),EVE_HASH=)$(EVE_HASH) DOCKER_ARCH_TAG=$(DOCKER_ARCH_TAG) ./tools/parse-pkgs.sh
LINUXKIT=$(CURDIR)/build-tools/bin/linuxkit
LINUXKIT_OPTS=--disable-content-trust $(if $(strip $(EVE_HASH)),--hash) $(EVE_HASH) $(if $(strip $(EVE_REL)),--release) $(EVE_REL) $(FORCE_BUILD)
LINUXKIT_PKG_TARGET=build
RESCAN_DEPS=FORCE
FORCE_BUILD=--force

ifeq ($(LINUXKIT_PKG_TARGET),push)
  EVE_REL:=$(shell git describe --always | grep -E '[0-9]*\.[0-9]*\.[0-9]*' || echo snapshot)
  ifneq ($(EVE_REL),snapshot)
    EVE_HASH:=$(EVE_REL)
    EVE_REL:=$(shell [ "`git tag | grep -E '[0-9]*\.[0-9]*\.[0-9]*' | sort -t. -n -k1,1 -k2,2 -k3,3 | tail -1`" = $(EVE_HASH) ] && echo latest)
  endif
endif

# We are currently filtering out a few packages from bulk builds
# since they are not getting published in Docker HUB
PKGS=$(shell ls -d pkg/* | grep -Ev "eve|test-microsvcs|u-boot")

# Top-level targets

all: help

test: $(GOBUILDER) | $(DIST)
	@echo Running tests on $(GOMODULE)
	@$(DOCKER_GO) "go test -v ./... 2>&1 | go-junit-report" $(GOTREE) $(GOMODULE) | sed -e '1d' > $(DIST)/results.xml

clean:
	rm -rf $(DIST) pkg/pillar/Dockerfile pkg/qrexec-lib/Dockerfile pkg/qrexec-dom0/Dockerfile \
	       images/installer.yml images/rootfs.yml.in

build-tools: $(LINUXKIT)
	@echo Done building $<

$(EFI_PART): $(LINUXKIT) | $(DIST)/bios
	cd $| ; $(DOCKER_UNPACK) $(shell $(LINUXKIT) pkg show-tag pkg/grub)-$(DOCKER_ARCH_TAG) EFI
	(echo "set root=(hd0)" ; echo "chainloader /EFI/BOOT/BOOTX64.EFI" ; echo boot) > $@/BOOT/grub.cfg

$(BIOS_IMG): $(LINUXKIT) | $(DIST)/bios
	cd $| ; $(DOCKER_UNPACK) $(shell $(LINUXKIT) pkg show-tag pkg/uefi)-$(DOCKER_ARCH_TAG) OVMF.fd

# run-installer
#
# This creates an image equivalent to live.img (called target.img)
# through the installer. It's the long road to live.img. Good for
# testing.
#
# -machine dumpdtb=virt.dtb 
#
run-installer-iso: $(BIOS_IMG)
	qemu-img create -f ${IMG_FORMAT} $(TARGET_IMG) ${MEDIA_SIZE}M
	$(QEMU_SYSTEM) $(QEMU_OPTS) -drive file=$(TARGET_IMG),format=$(IMG_FORMAT) -cdrom $(INSTALLER_IMG).iso -boot d

run-installer-raw: $(BIOS_IMG)
	qemu-img create -f ${IMG_FORMAT} $(TARGET_IMG) ${MEDIA_SIZE}M
	$(QEMU_SYSTEM) $(QEMU_OPTS) -drive file=$(TARGET_IMG),format=$(IMG_FORMAT) -drive file=$(INSTALLER_IMG).raw,format=raw

run-live run: $(BIOS_IMG)
	$(QEMU_SYSTEM) $(QEMU_OPTS) -drive file=$(LIVE_IMG).img,format=$(IMG_FORMAT)

run-target: $(BIOS_IMG)
	$(QEMU_SYSTEM) $(QEMU_OPTS) -drive file=$(TARGET_IMG),format=$(IMG_FORMAT)

run-rootfs: $(BIOS_IMG) $(EFI_PART)
	$(QEMU_SYSTEM) $(QEMU_OPTS) -drive file=$(ROOTFS_IMG),format=raw -drive file=fat:rw:$(EFI_PART)/..,format=raw 

run-grub: $(BIOS_IMG) $(EFI_PART)
	$(QEMU_SYSTEM) $(QEMU_OPTS) -drive file=fat:rw:$(EFI_PART)/..,format=raw

# ensure the dist directory exists
$(DIST) $(DIST)/bios:
	mkdir -p $@

# convenience targets - so you can do `make config` instead of `make dist/config.img`, and `make installer` instead of `make dist/amd64/installer.img
config: $(CONFIG_IMG)
rootfs: $(ROOTFS_IMG)
live: $(LIVE_IMG).img
installer: $(INSTALLER_IMG).raw
installer-iso: $(INSTALLER_IMG).iso

$(CONFIG_IMG): conf/server conf/onboard.cert.pem conf/wpa_supplicant.conf conf/authorized_keys conf/ | $(DIST)
	./tools/makeconfig.sh $(CONF_DIR) $@

$(ROOTFS_IMG): images/rootfs.yml | $(DIST)
	./tools/makerootfs.sh $< $(ROOTFS_FORMAT) $@
	@[ $$(wc -c < "$@") -gt $$(( 250 * 1024 * 1024 )) ] && \
          echo "ERROR: size of $@ is greater than 250MB (bigger than allocated partition)" && exit 1 || :

$(LIVE_IMG).img: $(LIVE_IMG).$(IMG_FORMAT) | $(DIST)
	@rm -f $@ >/dev/null 2>&1 || :
	ln -s $(notdir $<) $@

$(LIVE_IMG).qcow2: $(LIVE_IMG).raw | $(DIST)
	qemu-img convert -c -f raw -O qcow2 $< $@
	rm $<

$(LIVE_IMG).raw: $(ROOTFS_IMG) $(CONFIG_IMG) | $(DIST)
	tar -C $(DIST) -c $(notdir $^) | ./tools/makeflash.sh -C ${MEDIA_SIZE} $@

$(ROOTFS_IMG)_installer.img: images/installer.yml $(ROOTFS_IMG) $(CONFIG_IMG) | $(DIST)
	./tools/makerootfs.sh $< $(ROOTFS_FORMAT) $@
	@[ $$(wc -c < "$@") -gt $$(( 300 * 1024 * 1024 )) ] && \
          echo "ERROR: size of $@ is greater than 300MB (bigger than allocated partition)" && exit 1 || :

$(INSTALLER_IMG).raw: $(ROOTFS_IMG)_installer.img $(CONFIG_IMG) | $(DIST)
	tar -C $(DIST) -c $(notdir $^) | ./tools/makeflash.sh -C 350 $@ "efi imga conf_win"
	rm $(ROOTFS_IMG)_installer.img

$(INSTALLER_IMG).iso: images/installer.yml $(ROOTFS_IMG) $(CONFIG_IMG) | $(DIST)
	./tools/makeiso.sh $< $@

# top-level linuxkit packages targets, note the one enforcing ordering between packages
pkgs: RESCAN_DEPS=
pkgs: FORCE_BUILD=
pkgs: build-tools $(PKGS)
	@echo Done building packages

pkg/pillar: pkg/lisp pkg/xen-tools pkg/dnsmasq pkg/strongswan pkg/gpt-tools pkg/watchdog eve-pillar
	@true
pkg/qrexec-dom0: pkg/qrexec-lib pkg/xen-tools eve-qrexec-dom0
	@true
pkg/qrexec-lib: pkg/xen-tools eve-qrexec-lib
	@true
pkg/%: eve-% FORCE
	@true

eve: Makefile $(BIOS_IMG) $(CONFIG_IMG) $(INSTALLER_IMG).iso $(INSTALLER_IMG).raw $(ROOTFS_IMG) $(LIVE_IMG).img images/rootfs.yml images/installer.yml
	cp pkg/eve/* Makefile images/rootfs.yml images/installer.yml $(DIST)
	$(LINUXKIT) pkg $(LINUXKIT_PKG_TARGET) --hash-path $(CURDIR) $(LINUXKIT_OPTS) $(DIST)

sdk: $(addprefix proto-,$(PROTO_LANGS))
	mkdir -p $(GOTREE)/vendor/$(PKGBASE)
	cp -r sdk $(GOTREE)/vendor/$(PKGBASE)

proto-%: $(GOBUILDER)
	for sub in $(APIDIRS); do \
		mkdir -p sdk/$*/$$sub; \
		$(DOCKER_GO) "protoc -I/eve/api/$$sub --$*_out=paths=source_relative:/go/src/$(PKGBASE)/sdk/$*/$$sub /eve/api/$$sub/*.proto" $(CURDIR)/sdk/$*/ $(PKGBASE)/sdk/$*; \
	done

release:
	@function bail() { echo "ERROR: $$@" ; exit 1 ; } ;\
	 X=`echo $(VERSION) | cut -s -d. -f1` ; Y=`echo $(VERSION) | cut -s -d. -f2` ; Z=`echo $(VERSION) | cut -s -d. -f3` ;\
	 [ -z "$$X" -o -z "$$Y" -o -z "$$Z" ] && bail "VERSION missing (or incorrect). Re-run as: make VERSION=x.y.z $@" ;\
	 (git fetch && [ `git diff origin/master..master | wc -l` -eq 0 ]) || bail "origin/master is different from master" ;\
	 if git checkout $$X.$$Y 2>/dev/null ; then \
	    git merge origin/master ;\
	 else \
	    git checkout master -b $$X.$$Y && echo zedcloud.zededa.net > conf/server &&\
	    git commit -m"Setting default server to prod" conf/server ;\
	 fi || bail "Can't create $$X.$$Y branch" ;\
	 git tag -a -m"Release $$X.$$Y.$$Z" $$X.$$Y.$$Z &&\
	 echo "Done tagging $$X.$$Y.$$Z release. Check the branch with git log and then run" &&\
	 echo "  git push origin $$X.$$Y $$X.$$Y.$$Z"

shell: $(GOBUILDER)
	@$(DOCKER_GO) bash $(GOTREE) $(GOMODULE)

#
# Utility targets in support of our Dockerized build infrastrucutre
#
$(LINUXKIT): CGO_ENABLED=0
$(LINUXKIT): GOOS=$(shell uname -s | tr '[A-Z]' '[a-z]')
$(LINUXKIT): $(CURDIR)/build-tools/src/linuxkit/Gopkg.lock $(CURDIR)/build-tools/bin/manifest-tool $(GOBUILDER)
	@$(DOCKER_GO) "unset GOFLAGS ; unset GO111MODULE ; go build -ldflags '-X version.GitCommit=$(EVE_TREE_TAG)' -o /go/bin/linuxkit \
                          vendor/github.com/linuxkit/linuxkit/src/cmd/linuxkit" $(dir $<) / $(dir $@)
$(CURDIR)/build-tools/bin/manifest-tool: $(CURDIR)/build-tools/src/manifest-tool/Gopkg.lock
	@$(DOCKER_GO) "unset GOFLAGS ; unset GO111MODULE ; go build -ldflags '-X main.gitCommit=$(EVE_TREE_TAG)' -o /go/bin/manifest-tool \
                          vendor/github.com/estesp/manifest-tool" $(dir $<) / $(dir $@)

$(GOBUILDER):
ifneq ($(BUILD),local)
	@echo "Creating go builder image for user $(USER)"
	@docker build --build-arg GOVER=$(GOVER) --build-arg USER=$(USER) --build-arg GROUP=$(GROUP) \
                      --build-arg UID=$(UID) --build-arg GID=$(GID) -t $@ build-tools/src/scripts >/dev/null
	@echo "$@ docker container is ready to use"
endif

#
# Common, generalized rules
#
%.yml: %.yml.in build-tools $(RESCAN_DEPS)
	@$(PARSE_PKGS) $< > $@

%/Dockerfile: %/Dockerfile.in build-tools $(RESCAN_DEPS)
	@$(PARSE_PKGS) $< > $@

eve-%: pkg/%/Dockerfile build-tools $(RESCAN_DEPS)
	@$(LINUXKIT) pkg $(LINUXKIT_PKG_TARGET) $(LINUXKIT_OPTS) pkg/$*

%-show-tag:
	@$(LINUXKIT) pkg show-tag pkg/$*

%Gopkg.lock: %Gopkg.toml | $(GOBUILDER)
	@$(DOCKER_GO) "dep ensure -update $(GODEP_NAME)" $(dir $@)
	@echo Done updating $@

.PHONY: all clean test run pkgs help build-tools live rootfs config installer live FORCE $(DIST)
FORCE:

help:
	@echo "EVE is Edge Virtualization Engine"
	@echo
	@echo "This Makefile automates commons tasks of building and running"
	@echo "  * EVE"
	@echo "  * Installer of EVE"
	@echo "  * linuxkit command line tools"
	@echo "We currently support two platforms: x86_64 and aarch64. There is"
	@echo "even rudimentary support for cross-compiling that can be triggered"
	@echo "by forcing a particular architecture via adding ZARCH=[x86_64|aarch64]"
	@echo "to the make's command line. You can also run in a cross- way since"
	@echo "all the execution is done via qemu."
	@echo
	@echo "Commonly used maitenance and development targets:"
	@echo "   test           run EVE tests"
	@echo "   clean          clean build artifacts in a current directory (doesn't clean Docker)"
	@echo "   release        prepare branch for a release (VERSION=x.y.z required)"
	@echo "   shell          drop into docker container setup for Go development"
	@echo
	@echo "Commonly used build targets:"
	@echo "   build-tools    builds linuxkit and manifest-tool utilities under build-tools/bin"
	@echo "   config         builds a bundle with initial EVE configs"
	@echo "   pkgs           builds all EVE packages"
	@echo "   pkg/XXX        builds XXX EVE package"
	@echo "   rootfs         builds EVE rootfs image (upload it to the cloud as BaseImage)"
	@echo "   live           builds a full disk image of EVE which can be function as a virtual device"
	@echo "   installer      builds raw disk installer image (to be installed on bootable media)"
	@echo "   installer-iso  builds an ISO installers image (to be installed on bootable media)"
	@echo
	@echo "Commonly used run targets (note they don't automatically rebuild images they run):"
	@echo "   run-live          runs a full fledged virtual device on qemu (as close as it gets to actual h/w)"
	@echo "   run-rootfs        runs a rootfs.img (limited usefulness e.g. quick test before cloud upload)"
	@echo "   run-grub          runs our copy of GRUB bootloader and nothing else (very limited usefulness)"
	@echo "   run-installer-iso runs installer.iso (via qemu) and 'installs' EVE into (initially blank) target.img"
	@echo "   run-installer-raw runs installer.raw (via qemu) and 'installs' EVE into (initially blank) target.img"
	@echo "   run-target        runs a full fledged virtual device on qemu from target.img (similar to run-live)"
	@echo
	@echo "make run is currently an alias for make run-live"
	@echo
