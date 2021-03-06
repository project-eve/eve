// Copyright (c) 2017-2018 Zededa, Inc.
// SPDX-License-Identifier: Apache-2.0

package types

import (
	log "github.com/sirupsen/logrus"
	"github.com/zededa/eve/pkg/pillar/pubsub"
)

type LedBlinkCounter struct {
	BlinkCounter int
}

const (
	tmpDirName   = "/var/tmp/zededa/"
	ledConfigKey = "ledconfig"
)

// Global variable to supress log messages when nothing changes from this
// agent. Since other agents might have changed we still update the config.
var lastCount = 0

// Used by callers to change the behavior or the LED
func UpdateLedManagerConfig(count int) {
	blinkCount := LedBlinkCounter{
		BlinkCounter: count,
	}
	err := pubsub.PublishToDir(tmpDirName, ledConfigKey, &blinkCount)
	if err != nil {
		log.Errorln("err: ", err, tmpDirName)
	} else {
		if count != lastCount {
			log.Infof("UpdateLedManagerConfig: set %d\n", count)
			lastCount = count
		}
	}
}

// Merge the 1/2 values based on having usable addresses or not, with
// the value we get based on access to zedcloud or errors.
func DeriveLedCounter(ledCounter, usableAddressCount int) int {
	if usableAddressCount == 0 {
		return 1
	} else if ledCounter < 2 {
		return 2
	} else {
		return ledCounter
	}
}
