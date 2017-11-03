// Copyright (c) 2017 Zededa, Inc.
// All rights reserved.

// lisp configlet for overlay interface towards domU

package main

import (
	"fmt"
	"github.com/zededa/go-provision/types"
	"github.com/zededa/go-provision/wrap"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
)

// Need to fill in IID in 3 places
// Use this for the Mgmt IID
// XXX need to be able to set the ms name? Not needed for demo
const lispIIDtemplateMgmt = `
lisp map-server {
    dns-name = ms1.zededa.net
    authentication-key = test1_%d
    want-map-notify = yes
}

lisp map-server {
    dns-name = ms2.zededa.net
    authentication-key = test2_%d
    want-map-notify = yes
}
lisp map-cache {
    prefix {
        instance-id = %d
        eid-prefix = fd00::/8
        send-map-request = yes
    }
}
`

// Need to fill in IID in 5 places
// Use this for the application IIDs
const lispIIDtemplate = `
lisp map-server {
    ms-name = ms-%d
    dns-name = ms1.zededa.net
    authentication-key = test1_%d
    want-map-notify = yes
}

lisp map-server {
    ms-name = ms-%d
    dns-name = ms2.zededa.net
    authentication-key = test2_%d
    want-map-notify = yes
}
lisp map-cache {
    prefix {
        instance-id = %d
        eid-prefix = fd00::/8
        send-map-request = yes
    }
}
`

// Need to fill in (signature, additional, olIfname, IID)
// Use this for the Mgmt IID/EID
const lispEIDtemplateMgmt = `
lisp json {
    json-name = signature
    json-string = { "signature" : "%s" }
}

lisp json {
    json-name = additional-info
    json-string = %s
}

lisp interface {
    interface-name = overlay-mgmt
    device = %s
    instance-id = %d
}
`

// Need to pass in (IID, EID, rlocs), where rlocs is a string with
// sets of uplink info with:
// rloc {
//        interface = %s
// }
// rloc {
//        address = %s
// }
const lispDBtemplateMgmt = `
lisp database-mapping {
    prefix {
        instance-id = %d
        eid-prefix = %s/128
        signature-eid = yes
    }
    rloc {
        json-name = signature
        priority = 255
    }
    rloc {
        json-name = additional-info
        priority = 255
    }
%s
}
`

// Need to fill in (tag, signature, tag, additional, olifname, olifname, IID)
// Use this for the application EIDs
const lispEIDtemplate = `
lisp json {
    json-name = signature-%s
    json-string = { "signature" : "%s" }
}

lisp json {
    json-name = additional-info-%s
    json-string = %s
}

lisp interface {
    interface-name = overlay-%s
    device = %s
    instance-id = %d
}
`

// Need to fill in (IID, EID, IID, tag, tag, rlocs) where
// rlocs is a string with sets of uplink info with:
// rloc {
//        interface = %s
// }
// rloc {
//        address = %s
//        priority = %d
// }
const lispDBtemplate = `
lisp database-mapping {
    prefix {
        instance-id = %d
        eid-prefix = %s/128
        ms-name = ms-%d
    }
    rloc {
        json-name = signature-%s
        priority = 255
    }
    rloc {
        json-name = additional-info-%s
        priority = 255
    }
%s
}
`

const baseFilename = "/opt/zededa/etc/lisp.config.base"
const destFilename = "/opt/zededa/lisp/lisp.config"
const RestartCmd = "/opt/zededa/lisp/RESTART-LISP"
const StopCmd = "/opt/zededa/lisp/STOP-LISP"
const RLFilename = "/opt/zededa/lisp/RL"

// We write files with the IID-specifics (and not EID) to files
// in <globalRunDirname>/lisp/<iid>.
// We write files with the EID-specifics to files named
// <globalRunDirname>/lisp/<eid>.
// We concatenate all of those to baseFilename and store the result
// in destFilename
//
// Would be more polite to return an error then to Fatal
func createLispConfiglet(lispRunDirname string, isMgmt bool, IID uint32,
	EID net.IP, lispSignature string,
	globalStatus types.DeviceNetworkStatus,
	tag string, olIfname string, additionalInfo string) {
	fmt.Printf("createLispConfiglet: %s %v %d %s %v %s %s %s %s\n",
		lispRunDirname, isMgmt, IID, EID, lispSignature, globalStatus,
		tag, olIfname, additionalInfo)
	cfgPathnameIID := lispRunDirname + "/" +
		strconv.FormatUint(uint64(IID), 10)
	file1, err := os.Create(cfgPathnameIID)
	if err != nil {
		log.Fatal("os.Create for ", cfgPathnameIID, err)
	}
	defer file1.Close()

	var cfgPathnameEID string
	if isMgmt {
		// LISP gets confused if the management "lisp interface"
		// isn't first in the list. Force that for now.
		cfgPathnameEID = lispRunDirname + "/0-" + EID.String()
	} else {
		cfgPathnameEID = lispRunDirname + "/" + EID.String()
	}
	file2, err := os.Create(cfgPathnameEID)
	if err != nil {
		log.Fatal("os.Create for ", cfgPathnameEID, err)
	}
	defer file2.Close()
	rlocString := ""
	for _, u := range globalStatus.Uplink {
		one := fmt.Sprintf("    rloc {\n        interface = %s\n    }\n", u)
		rlocString += one
	}
	for _, a := range globalStatus.UplinkAddrs {
		prio := 0
		// XXX don't generate IPv6 UDP checksum hence lower priority
		// for now
		if a.IsLinkLocalUnicast() {
			prio = 2
		} else if a.To4() == nil {
			prio = 255
		}
		one := fmt.Sprintf("    rloc {\n        address = %s\n        priority = %d\n    }\n", a, prio)
		rlocString += one
	}
	if isMgmt {
		file1.WriteString(fmt.Sprintf(lispIIDtemplateMgmt, IID, IID,
			IID))
		file2.WriteString(fmt.Sprintf(lispEIDtemplateMgmt,
			lispSignature, additionalInfo, olIfname, IID))
		file2.WriteString(fmt.Sprintf(lispDBtemplateMgmt,
			IID, EID, rlocString))
	} else {
		file1.WriteString(fmt.Sprintf(lispIIDtemplate,
			IID, IID, IID, IID, IID))
		file2.WriteString(fmt.Sprintf(lispEIDtemplate,
			tag, lispSignature, tag, additionalInfo, olIfname,
			olIfname, IID))
		file2.WriteString(fmt.Sprintf(lispDBtemplate,
			IID, EID, IID, tag, tag, rlocString))
	}
	updateLisp(lispRunDirname, globalStatus.Uplink)
}

func updateLispConfiglet(lispRunDirname string, isMgmt bool, IID uint32,
	EID net.IP, lispSignature string,
 	globalStatus types.DeviceNetworkStatus,
	tag string, olIfname string, additionalInfo string) {
	fmt.Printf("updateLispConfiglet: %s %v %d %s %v %s %s %s %s\n",
		lispRunDirname, isMgmt, IID, EID, lispSignature, globalStatus,
		tag, olIfname, additionalInfo)
	createLispConfiglet(lispRunDirname, isMgmt, IID, EID, lispSignature,
		globalStatus, tag, olIfname, additionalInfo)
}

func deleteLispConfiglet(lispRunDirname string, isMgmt bool, IID uint32,
	EID net.IP, globalStatus types.DeviceNetworkStatus) {
	fmt.Printf("deleteLispConfiglet: %s %d %s %v\n",
		lispRunDirname, IID, EID, globalStatus)

	var cfgPathnameEID string
	if isMgmt {
		// LISP gets confused if the management "lisp interface"
		// isn't first in the list. Force that for now.
		cfgPathnameEID = lispRunDirname + "/0-" + EID.String()
	} else {
		cfgPathnameEID = lispRunDirname + "/" + EID.String()
	}
	if err := os.Remove(cfgPathnameEID); err != nil {
		log.Println(err)
	}

	// XXX can't delete IID file unless refcnt since other EIDs
	// can refer to it.
	// cfgPathnameIID := lispRunDirname + "/" +
	//	strconv.FormatUint(uint64(IID), 10)

	updateLisp(lispRunDirname, globalStatus.Uplink)
}

func updateLisp(lispRunDirname string, upLinkIfnames []string) {
	fmt.Printf("updateLisp: %s %v\n", lispRunDirname, upLinkIfnames)

	if deferUpdate {
		log.Printf("updateLisp deferred\n")
		deferLispRunDirname = lispRunDirname
		deferUpLinkIfnames = upLinkIfnames
		return
	}

	tmpfile, err := ioutil.TempFile("/tmp/", "lisp")
	if err != nil {
		log.Println("TempFile ", err)
		return
	}
	defer tmpfile.Close()
	defer os.Remove(tmpfile.Name())

	fmt.Printf("Copying from %s to %s\n", baseFilename, tmpfile.Name())
	s, err := os.Open(baseFilename)
	if err != nil {
		log.Println("os.Open ", baseFilename, err)
		return
	}
	defer s.Close()
	var cnt int64
	if cnt, err = io.Copy(tmpfile, s); err != nil {
		log.Println("io.Copy ", baseFilename, err)
		return
	}
	fmt.Printf("Copied %d bytes from %s\n", cnt, baseFilename)
	files, err := ioutil.ReadDir(lispRunDirname)
	if err != nil {
		log.Println(err)
		return
	}
	eidCount := 0
	for _, file := range files {
		// The IID files are named by the IID hence an integer
		if _, err := strconv.Atoi(file.Name()); err != nil {
			eidCount += 1
		}
		filename := lispRunDirname + "/" + file.Name()
		fmt.Printf("Copying from %s to %s\n", filename, tmpfile.Name())
		s, err := os.Open(filename)
		if err != nil {
			log.Println("os.Open ", filename, err)
			return
		}
		defer s.Close()
		if cnt, err = io.Copy(tmpfile, s); err != nil {
			log.Println("io.Copy ", filename, err)
			return
		}
		fmt.Printf("Copied %d bytes from %s\n", cnt, filename)
	}
	if err := tmpfile.Close(); err != nil {
		log.Println("Close ", tmpfile.Name(), err)
		return
	}
	// This seems safer; make sure it is stopped before rewriting file
	stopLisp()

	if err := os.Rename(tmpfile.Name(), destFilename); err != nil {
		log.Println("Rename ", tmpfile.Name(), destFilename, err)
		return
	}

	// Determine the set of devices from the above config file
	grep := wrap.Command("grep", "device = ", destFilename)
	awk := wrap.Command("awk", "{print $NF}")
	awk.Stdin, _ = grep.StdoutPipe()
	if err := grep.Start(); err != nil {
		log.Println("grep.Start failed: ", err)
		return
	}
	intfs, err := awk.Output()
	if err != nil {
		log.Println("awk.Output failed: ", err)
		return
	}
	_ = grep.Wait()
	_ = awk.Wait()
	devices := strings.TrimSpace(string(intfs))
	devices = strings.Replace(devices, "\n", " ", -1)
	fmt.Printf("updateLisp: found %d EIDs devices <%v>\n", eidCount, devices)

	// Check how many EIDs we have configured. If none we stop lisp
	if eidCount == 0 {
		stopLisp()
	} else {
		restartLisp(upLinkIfnames, devices)
	}
}

var deferUpdate = false
var deferLispRunDirname = ""
var deferUpLinkIfnames []string = nil

func handleLispRestart(done bool) {
	log.Printf("handleLispRestart(%v)\n", done)
	if done {
		if deferUpdate {
			deferUpdate = false
			if deferLispRunDirname != "" {
				updateLisp(deferLispRunDirname,
					deferUpLinkIfnames)
				deferLispRunDirname = ""
				deferUpLinkIfnames = nil
			}
		}
	} else {
		deferUpdate = true
	}
}

func restartLisp(upLinkIfnames []string, devices string) {
	log.Printf("restartLisp: %v %s\n",
		upLinkIfnames, devices)
	// XXX how to restart with multiple uplinks?
	args := []string{
		RestartCmd,
		"8080",
		upLinkIfnames[0],
	}
	itrTimeout := 1
	cmd := wrap.Command(RestartCmd)
	cmd.Args = args
	env := os.Environ()
	env = append(env, fmt.Sprintf("LISP_NO_IPTABLES="))
	env = append(env, fmt.Sprintf("LISP_PCAP_LIST=%s", devices))
	// Make sure the ITR doesn't give up to early; maybe it should
	// wait forever? Will we be dead for this time?
	env = append(env, fmt.Sprintf("LISP_ITR_WAIT_TIME=%d", itrTimeout))
	cmd.Env = env
	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("RESTART-LISP failed ", err)
		log.Printf("RESTART-LISP output %s\n", string(stdoutStderr))
		return
	}
	log.Printf("restartLisp done: output %s\n", string(stdoutStderr))

	// Save the restart as a bash command called RL
	const RLTemplate = "#!/bin/bash\n# Automatically generated by zedrouter\ncd `dirname $0`\nLISP_NO_IPTABLES=,LISP_PCAP_LIST='%s',LISP_ITR_WAIT_TIME=%d %s 8080 %s\n"
	// XXX how to restart with multiple uplinks?
	b := []byte(fmt.Sprintf(RLTemplate, devices, itrTimeout, RestartCmd,
		upLinkIfnames[0]))
	err = ioutil.WriteFile(RLFilename, b, 0744)
	if err != nil {
		log.Fatal("WriteFile", err, RLFilename)
		return
	}
	fmt.Printf("Wrote %s\n", RLFilename)
}

func stopLisp() {
	log.Printf("stopLisp\n")
	cmd := wrap.Command(StopCmd)
	env := os.Environ()
	env = append(env, fmt.Sprintf("LISP_NO_IPTABLES="))
	cmd.Env = env
	stdoutStderr, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("STOP-LISP failed ", err)
		log.Printf("STOP-LISP output %s\n", string(stdoutStderr))
		return
	}
	log.Printf("stopLisp done: output %s\n", string(stdoutStderr))
	if err = os.Remove(RLFilename); err != nil {
		log.Println(err)
		return
	}
	log.Printf("Removed %s\n", RLFilename)
}