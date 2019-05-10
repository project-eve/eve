#!/bin/sh
#
# Copyright (c) 2018 Zededa, Inc.
# SPDX-License-Identifier: Apache-2.0
#
# Set export RELEASE=1 to get just the git tag as the version
# XXX not clear how we can feed a RELEASE environment into the docker

GIT_TAG=$(git tag | tail -1)
BUILD_DATE=$(date -u +"%Y-%m-%d.%H.%M")
GIT_VERSION=$(git describe --match v --abbrev=8 --always --dirty)
BRANCH_NAME=$(git rev-parse --abbrev-ref HEAD)

if [ -n "${RELEASE}" ]; then
        EXTRA_VERSION=""
else
        EXTRA_VERSION=-${GIT_VERSION}-${BUILD_DATE}
fi

if [ "${BRANCH_NAME}" = "master" ]; then
        BUILD_VERSION=${GIT_TAG}${EXTRA_VERSION}
else
        BUILD_VERSION=${GIT_TAG}-${GIT_BRANCH}${EXTRA_VERSION}
fi
echo "${BUILD_VERSION}"
