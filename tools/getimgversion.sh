#!/bin/sh
# Run against a rootfs.img

ROOTFS=rootfs.img
PRINTALL=0

while [ $# != 0 ]; do
    if [ $1 = "-a" ]; then
	PRINTALL=1
    else
	ROOTFS=$1
    fi
    shift
done

sudo mount -o loop ${ROOTFS} /mnt
VERS=`sudo cat /mnt/containers/services/pillar/lower/opt/zededa/bin/versioninfo`
ZA=`sudo file -L /mnt/containers/services/pillar/lower/opt/zededa/bin/zedagent`
LZ=`sudo file /mnt/containers/services/pillar/lower/opt/zededa/bin/lisp-ztr`
sudo umount /mnt
echo $VERS
if [ $PRINTALL = 1 ]; then
    echo "file zedagent: $ZA"
    echo "file lisp-ztr: $LZ"
fi
