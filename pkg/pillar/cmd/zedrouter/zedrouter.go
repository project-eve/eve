// Copyright (c) 2017-2018 Zededa, Inc.
// SPDX-License-Identifier: Apache-2.0

// Process input in the form of a collection of AppNetworkConfig structs
// from zedmanager and zedagent. Publish the status as AppNetworkStatus.
// Produce the updated configlets (for radvd, dnsmasq, ip*tables, lisp.config,
// ipset, ip link/addr/route configuration) based on that and apply those
// configlets.

package zedrouter

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/eriknordmark/netlink"
	"github.com/google/go-cmp/cmp"
	"github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/zededa/eve/pkg/pillar/agentlog"
	"github.com/zededa/eve/pkg/pillar/cast"
	"github.com/zededa/eve/pkg/pillar/devicenetwork"
	"github.com/zededa/eve/pkg/pillar/flextimer"
	"github.com/zededa/eve/pkg/pillar/iptables"
	"github.com/zededa/eve/pkg/pillar/pidfile"
	"github.com/zededa/eve/pkg/pillar/pubsub"
	"github.com/zededa/eve/pkg/pillar/types"
	"github.com/zededa/eve/pkg/pillar/wrap"
)

const (
	agentName     = "zedrouter"
	runDirname    = "/var/run/zedrouter"
	tmpDirname    = "/var/tmp/zededa"
	DataPlaneName = "lisp-ztr"
)

// Set from Makefile
var Version = "No version specified"

type zedrouterContext struct {
	// Legacy data plane enable/disable flag
	legacyDataPlane bool

	subNetworkObjectConfig  *pubsub.Subscription
	subNetworkServiceConfig *pubsub.Subscription

	pubNetworkObjectStatus  *pubsub.Publication
	pubNetworkServiceStatus *pubsub.Publication

	subAppNetworkConfig   *pubsub.Subscription
	subAppNetworkConfigAg *pubsub.Subscription // From zedagent for dom0

	pubAppNetworkStatus *pubsub.Publication

	pubLispDataplaneConfig *pubsub.Publication
	subLispInfoStatus      *pubsub.Subscription
	subLispMetrics         *pubsub.Subscription

	assignableAdapters       *types.AssignableAdapters
	subAssignableAdapters    *pubsub.Subscription
	pubNetworkServiceMetrics *pubsub.Publication
	subDeviceNetworkStatus   *pubsub.Subscription
	deviceNetworkStatus      *types.DeviceNetworkStatus
	ready                    bool
	subGlobalConfig          *pubsub.Subscription
	pubUuidToNum             *pubsub.Publication

	// NetworkInstance
	subNetworkInstanceConfig  *pubsub.Subscription
	pubNetworkInstanceStatus  *pubsub.Publication
	pubNetworkInstanceMetrics *pubsub.Publication
	networkInstanceStatusMap  map[uuid.UUID]*types.NetworkInstanceStatus
}

var debug = false
var debugOverride bool // From command line arg

func Run() {
	versionPtr := flag.Bool("v", false, "Version")
	debugPtr := flag.Bool("d", false, "Debug flag")
	curpartPtr := flag.String("c", "", "Current partition")
	flag.Parse()
	debug = *debugPtr
	debugOverride = debug
	if debugOverride {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	curpart := *curpartPtr
	if *versionPtr {
		fmt.Printf("%s: %s\n", os.Args[0], Version)
		return
	}
	logf, err := agentlog.Init(agentName, curpart)
	if err != nil {
		log.Fatal(err)
	}
	defer logf.Close()

	if err := pidfile.CheckAndCreatePidfile(agentName); err != nil {
		log.Fatal(err)
	}
	log.Infof("Starting %s\n", agentName)

	// Run a periodic timer so we always update StillRunning
	stillRunning := time.NewTicker(25 * time.Second)
	agentlog.StillRunning(agentName)

	if _, err := os.Stat(runDirname); err != nil {
		log.Infof("Create %s\n", runDirname)
		if err := os.Mkdir(runDirname, 0755); err != nil {
			log.Fatal(err)
		}
	} else {
		// dnsmasq needs to read as nobody
		if err := os.Chmod(runDirname, 0755); err != nil {
			log.Fatal(err)
		}
	}

	pubUuidToNum, err := pubsub.PublishPersistent(agentName,
		types.UuidToNum{})
	if err != nil {
		log.Fatal(err)
	}
	pubUuidToNum.ClearRestarted()

	// Pick up (mostly static) AssignableAdapters before we process
	// any Routes; Pbr needs to know which network adapters are assignable

	aa := types.AssignableAdapters{}
	zedrouterCtx := zedrouterContext{
		legacyDataPlane:    false,
		assignableAdapters: &aa,
	}
	zedrouterCtx.networkInstanceStatusMap =
		make(map[uuid.UUID]*types.NetworkInstanceStatus)

	subDeviceNetworkStatus, err := pubsub.Subscribe("nim",
		types.DeviceNetworkStatus{}, false, &zedrouterCtx)
	if err != nil {
		log.Fatal(err)
	}
	subDeviceNetworkStatus.ModifyHandler = handleDNSModify
	subDeviceNetworkStatus.DeleteHandler = handleDNSDelete
	zedrouterCtx.subDeviceNetworkStatus = subDeviceNetworkStatus
	subDeviceNetworkStatus.Activate()

	subAssignableAdapters, err := pubsub.Subscribe("domainmgr",
		types.AssignableAdapters{}, false, &zedrouterCtx)
	if err != nil {
		log.Fatal(err)
	}
	subAssignableAdapters.ModifyHandler = handleAAModify
	subAssignableAdapters.DeleteHandler = handleAADelete
	zedrouterCtx.subAssignableAdapters = subAssignableAdapters
	subAssignableAdapters.Activate()

	// Look for global config such as log levels
	subGlobalConfig, err := pubsub.Subscribe("", types.GlobalConfig{},
		false, &zedrouterCtx)
	if err != nil {
		log.Fatal(err)
	}
	subGlobalConfig.ModifyHandler = handleGlobalConfigModify
	subGlobalConfig.DeleteHandler = handleGlobalConfigDelete
	zedrouterCtx.subGlobalConfig = subGlobalConfig
	subGlobalConfig.Activate()

	zedrouterCtx.deviceNetworkStatus = &types.DeviceNetworkStatus{}
	zedrouterCtx.pubUuidToNum = pubUuidToNum

	// Create publish before subscribing and activating subscriptions
	// Also need to do this before we wait for IP addresses since
	// zedagent waits for these to be published/exist, and zedagent
	// runs the fallback timers after that wait.
	pubNetworkObjectStatus, err := pubsub.Publish(agentName,
		types.NetworkObjectStatus{})
	if err != nil {
		log.Fatal(err)
	}
	zedrouterCtx.pubNetworkObjectStatus = pubNetworkObjectStatus

	pubNetworkServiceStatus, err := pubsub.Publish(agentName,
		types.NetworkServiceStatus{})
	if err != nil {
		log.Fatal(err)
	}
	zedrouterCtx.pubNetworkServiceStatus = pubNetworkServiceStatus

	pubNetworkInstanceStatus, err := pubsub.Publish(agentName,
		types.NetworkInstanceStatus{})
	if err != nil {
		log.Fatal(err)
	}
	zedrouterCtx.pubNetworkInstanceStatus = pubNetworkInstanceStatus

	pubAppNetworkStatus, err := pubsub.Publish(agentName,
		types.AppNetworkStatus{})
	if err != nil {
		log.Fatal(err)
	}
	zedrouterCtx.pubAppNetworkStatus = pubAppNetworkStatus
	pubAppNetworkStatus.ClearRestarted()

	pubLispDataplaneConfig, err := pubsub.Publish(agentName,
		types.LispDataplaneConfig{})
	if err != nil {
		log.Fatal(err)
	}
	zedrouterCtx.pubLispDataplaneConfig = pubLispDataplaneConfig

	pubNetworkServiceMetrics, err := pubsub.Publish(agentName,
		types.NetworkServiceMetrics{})
	if err != nil {
		log.Fatal(err)
	}
	zedrouterCtx.pubNetworkServiceMetrics = pubNetworkServiceMetrics

	pubNetworkInstanceMetrics, err := pubsub.Publish(agentName,
		types.NetworkInstanceMetrics{})
	if err != nil {
		log.Fatal(err)
	}
	zedrouterCtx.pubNetworkInstanceMetrics = pubNetworkInstanceMetrics

	appNumAllocatorInit(&zedrouterCtx)
	bridgeNumAllocatorInit(&zedrouterCtx)
	handleInit(runDirname)

	// Before we process any NetworkServices we want to know the
	// assignable adapters.
	for !zedrouterCtx.assignableAdapters.Initialized {
		log.Infof("Waiting for AssignableAdapters\n")
		select {
		case change := <-subGlobalConfig.C:
			subGlobalConfig.ProcessChange(change)

		case change := <-subAssignableAdapters.C:
			subAssignableAdapters.ProcessChange(change)

		case change := <-subDeviceNetworkStatus.C:
			subDeviceNetworkStatus.ProcessChange(change)
		}
	}
	log.Infof("Have %d assignable adapters\n", len(aa.IoBundleList))

	// Subscribe to network objects and services from zedagent
	subNetworkObjectConfig, err := pubsub.Subscribe("zedagent",
		types.NetworkObjectConfig{}, false, &zedrouterCtx)
	if err != nil {
		log.Fatal(err)
	}
	subNetworkObjectConfig.ModifyHandler = handleNetworkObjectModify
	subNetworkObjectConfig.DeleteHandler = handleNetworkObjectDelete
	zedrouterCtx.subNetworkObjectConfig = subNetworkObjectConfig
	subNetworkObjectConfig.Activate()

	subNetworkServiceConfig, err := pubsub.Subscribe("zedagent",
		types.NetworkServiceConfig{}, false, &zedrouterCtx)
	if err != nil {
		log.Fatal(err)
	}
	subNetworkServiceConfig.ModifyHandler = handleNetworkServiceModify
	subNetworkServiceConfig.DeleteHandler = handleNetworkServiceDelete
	zedrouterCtx.subNetworkServiceConfig = subNetworkServiceConfig
	subNetworkServiceConfig.Activate()

	subNetworkInstanceConfig, err := pubsub.Subscribe("zedagent",
		types.NetworkInstanceConfig{}, false, &zedrouterCtx)
	if err != nil {
		log.Fatal(err)
	}
	subNetworkInstanceConfig.ModifyHandler = handleNetworkInstanceModify
	subNetworkInstanceConfig.DeleteHandler = handleNetworkInstanceDelete
	zedrouterCtx.subNetworkInstanceConfig = subNetworkInstanceConfig
	subNetworkInstanceConfig.Activate()
	log.Infof("Subscribed to NetworkInstanceConfig")

	// Subscribe to AppNetworkConfig from zedmanager and from zedagent
	subAppNetworkConfig, err := pubsub.Subscribe("zedmanager",
		types.AppNetworkConfig{}, false, &zedrouterCtx)
	if err != nil {
		log.Fatal(err)
	}
	subAppNetworkConfig.ModifyHandler = handleAppNetworkConfigModify
	subAppNetworkConfig.DeleteHandler = handleAppNetworkConfigDelete
	subAppNetworkConfig.RestartHandler = handleRestart
	zedrouterCtx.subAppNetworkConfig = subAppNetworkConfig
	subAppNetworkConfig.Activate()

	// Subscribe to AppNetworkConfig from zedmanager
	subAppNetworkConfigAg, err := pubsub.Subscribe("zedagent",
		types.AppNetworkConfig{}, false, &zedrouterCtx)
	if err != nil {
		log.Fatal(err)
	}
	subAppNetworkConfigAg.ModifyHandler = handleAppNetworkConfigModify
	subAppNetworkConfigAg.DeleteHandler = handleAppNetworkConfigDelete
	zedrouterCtx.subAppNetworkConfigAg = subAppNetworkConfigAg
	subAppNetworkConfigAg.Activate()

	subLispInfoStatus, err := pubsub.Subscribe("lisp-ztr",
		types.LispInfoStatus{}, false, &zedrouterCtx)
	if err != nil {
		log.Fatal(err)
	}
	subLispInfoStatus.ModifyHandler = handleLispInfoModify
	subLispInfoStatus.DeleteHandler = handleLispInfoDelete
	zedrouterCtx.subLispInfoStatus = subLispInfoStatus
	subLispInfoStatus.Activate()

	subLispMetrics, err := pubsub.Subscribe("lisp-ztr",
		types.LispMetrics{}, false, &zedrouterCtx)
	if err != nil {
		log.Fatal(err)
	}
	subLispMetrics.ModifyHandler = handleLispMetricsModify
	subLispMetrics.DeleteHandler = handleLispMetricsDelete
	zedrouterCtx.subLispMetrics = subLispMetrics
	subLispMetrics.Activate()

	PbrInit(&zedrouterCtx)
	routeChanges := devicenetwork.RouteChangeInit()
	addrChanges := devicenetwork.AddrChangeInit()
	linkChanges := devicenetwork.LinkChangeInit()

	// Publish network metrics for zedagent every 10 seconds
	nms := getNetworkMetrics(&zedrouterCtx) // Need type of data
	pub, err := pubsub.Publish(agentName, nms)
	if err != nil {
		log.Fatal(err)
	}
	interval := time.Duration(10 * time.Second)
	max := float64(interval)
	min := max * 0.3
	publishTimer := flextimer.NewRangeTicker(time.Duration(min),
		time.Duration(max))

	updateLispConfiglets(&zedrouterCtx, zedrouterCtx.legacyDataPlane)

	setFreeMgmtPorts(types.GetMgmtPortsFree(*zedrouterCtx.deviceNetworkStatus, 0))

	zedrouterCtx.ready = true
	log.Infof("zedrouterCtx.ready\n")

	// First wait for restarted from zedmanager to
	// reduce the number of LISP-RESTARTs
	for !subAppNetworkConfig.Restarted() {
		log.Infof("Waiting for zedmanager to report restarted\n")
		select {
		case change := <-subGlobalConfig.C:
			subGlobalConfig.ProcessChange(change)

		case change := <-subAssignableAdapters.C:
			subAssignableAdapters.ProcessChange(change)

		case change := <-subAppNetworkConfig.C:
			subAppNetworkConfig.ProcessChange(change)

		case change := <-subDeviceNetworkStatus.C:
			subDeviceNetworkStatus.ProcessChange(change)

		case change := <-subNetworkInstanceConfig.C:
			log.Infof("AppNetworkConfig - waiting to Restart - "+
				"InstanceConfig change at %+v", time.Now())
			subNetworkInstanceConfig.ProcessChange(change)
		}
	}
	log.Infof("Zedmanager has restarted. Entering main Select loop\n")

	for {
		select {
		case change := <-subGlobalConfig.C:
			subGlobalConfig.ProcessChange(change)

		case change := <-subAssignableAdapters.C:
			subAssignableAdapters.ProcessChange(change)

		case change := <-subAppNetworkConfig.C:
			subAppNetworkConfig.ProcessChange(change)

		case change := <-subAppNetworkConfigAg.C:
			subAppNetworkConfigAg.ProcessChange(change)

		case change := <-subDeviceNetworkStatus.C:
			subDeviceNetworkStatus.ProcessChange(change)

		case change, ok := <-addrChanges:
			if !ok {
				log.Errorf("addrChanges closed\n")
				addrChanges = devicenetwork.AddrChangeInit()
				break
			}
			ifname := PbrAddrChange(zedrouterCtx.deviceNetworkStatus,
				change)
			if ifname != "" &&
				!types.IsMgmtPort(*zedrouterCtx.deviceNetworkStatus,
					ifname) {
				log.Debugf("addrChange(%s) not mgmt port\n", ifname)
				// Even if ethN isn't individually assignable, it
				// could be used for a bridge.
				maybeUpdateBridgeIPAddr(&zedrouterCtx, ifname)
				maybeUpdateBridgeIPAddrForNetworkInstance(
					&zedrouterCtx, ifname)
			}
		case change, ok := <-linkChanges:
			if !ok {
				log.Errorf("linkChanges closed\n")
				linkChanges = devicenetwork.LinkChangeInit()
				break
			}
			ifname := PbrLinkChange(zedrouterCtx.deviceNetworkStatus,
				change)
			if ifname != "" &&
				!types.IsMgmtPort(*zedrouterCtx.deviceNetworkStatus,
					ifname) {
				log.Debugf("linkChange(%s) not mgmt port\n", ifname)
				// Even if ethN isn't individually assignable, it
				// could be used for a bridge.
				maybeUpdateBridgeIPAddr(&zedrouterCtx, ifname)
				maybeUpdateBridgeIPAddrForNetworkInstance(
					&zedrouterCtx, ifname)
			}
		case change, ok := <-routeChanges:
			if !ok {
				log.Errorf("routeChanges closed\n")
				routeChanges = devicenetwork.RouteChangeInit()
				break
			}
			PbrRouteChange(zedrouterCtx.deviceNetworkStatus, change)

		case <-publishTimer.C:
			log.Debugln("publishTimer at", time.Now())
			err := pub.Publish("global",
				getNetworkMetrics(&zedrouterCtx))
			if err != nil {
				log.Errorf("getNetworkMetrics failed %s\n", err)
			}
			publishNetworkServiceStatusAll(&zedrouterCtx)
			publishNetworkInstanceMetricsAll(&zedrouterCtx)

		case change := <-subNetworkObjectConfig.C:
			subNetworkObjectConfig.ProcessChange(change)

		case change := <-subNetworkServiceConfig.C:
			subNetworkServiceConfig.ProcessChange(change)

		case change := <-subNetworkInstanceConfig.C:
			log.Infof("NetworkInstanceConfig change at %+v", time.Now())
			subNetworkInstanceConfig.ProcessChange(change)

		case change := <-subLispInfoStatus.C:
			subLispInfoStatus.ProcessChange(change)

		case change := <-subLispMetrics.C:
			subLispMetrics.ProcessChange(change)

		case <-stillRunning.C:
			agentlog.StillRunning(agentName)
		}
	}
}

func maybeHandleDNS(ctx *zedrouterContext) {
	if !ctx.ready {
		return
	}
	updateLispConfiglets(ctx, ctx.legacyDataPlane)

	setFreeMgmtPorts(types.GetMgmtPortsFree(*ctx.deviceNetworkStatus, 0))
	// XXX do a NatInactivate/NatActivate if management ports changed?
}

func handleRestart(ctxArg interface{}, done bool) {

	log.Debugf("handleRestart(%v)\n", done)
	ctx := ctxArg.(*zedrouterContext)
	if ctx.ready {
		handleLispRestart(done, ctx.legacyDataPlane)
	}
	if done {
		// Since all work is done inline we can immediately say that
		// we have restarted.
		ctx.pubAppNetworkStatus.SignalRestarted()
	}
}

var lispRunDirname string

// XXX hack to avoid the pslisp hang on Erik's laptop
var broken = false

func handleInit(runDirname string) {

	// XXX should this be in the lisp code?
	lispRunDirname = runDirname + "/lisp"
	if _, err := os.Stat(lispRunDirname); err != nil {
		log.Debugf("Create %s\n", lispRunDirname)
		if err := os.Mkdir(lispRunDirname, 0700); err != nil {
			log.Fatal(err)
		}
	}
	// XXX should this be in dnsmasq code?
	// Need to make sure we don't have any stale leases
	leasesFile := "/var/lib/misc/dnsmasq.leases"
	if _, err := os.Stat(leasesFile); err == nil {
		if err := os.Remove(leasesFile); err != nil {
			log.Fatal(err)
		}
	}

	// Setup initial iptables rules
	iptables.IptablesInit()

	// ipsets which are independent of config
	createDefaultIpset()

	_, err := wrap.Command("sysctl", "-w",
		"net.ipv4.ip_forward=1").Output()
	if err != nil {
		log.Fatal("Failed setting ip_forward ", err)
	}
	_, err = wrap.Command("sysctl", "-w",
		"net.ipv6.conf.all.forwarding=1").Output()
	if err != nil {
		log.Fatal("Failed setting ipv6.conf.all.forwarding ", err)
	}
	// We use ip6tables for the bridge
	_, err = wrap.Command("sysctl", "-w",
		"net.bridge.bridge-nf-call-ip6tables=1").Output()
	if err != nil {
		log.Fatal("Failed setting net.bridge-nf-call-ip6tables ", err)
	}
	_, err = wrap.Command("sysctl", "-w",
		"net.bridge.bridge-nf-call-iptables=1").Output()
	if err != nil {
		log.Fatal("Failed setting net.bridge-nf-call-iptables ", err)
	}
	_, err = wrap.Command("sysctl", "-w",
		"net.bridge.bridge-nf-call-arptables=1").Output()
	if err != nil {
		log.Fatal("Failed setting net.bridge-nf-call-arptables ", err)
	}
	// Needed in test setups where we have the same subnet on multiple ports
	// XXX restrict to management ports?
	_, err = wrap.Command("sysctl", "-w",
		"net.ipv4.conf.all.rp_filter=2").Output()
	if err != nil {
		log.Fatal("Failed setting rp_filter ", err)
	}
	_, err = wrap.Command("sysctl", "-w",
		"net.ipv4.conf.all.log_martians=1").Output()
	if err != nil {
		log.Fatal("Failed setting log_martians ", err)
	}
	_, err = wrap.Command("sysctl", "-w",
		"net.ipv4.conf.default.log_martians=1").Output()
	if err != nil {
		log.Fatal("Failed setting log_martians ", err)
	}

	// XXX hack to determine whether a real system or Erik's laptop
	_, err = wrap.Command("xl", "list").Output()
	if err != nil {
		log.Errorf("Command xl list failed: %s\n", err)
		broken = true
	}
}

func publishLispDataplaneConfig(ctx *zedrouterContext,
	status *types.LispDataplaneConfig) {
	key := "global"
	log.Debugf("publishLispDataplaneConfig(%s)\n", key)
	pub := ctx.pubLispDataplaneConfig
	pub.Publish(key, status)
}

func publishAppNetworkStatus(ctx *zedrouterContext,
	status *types.AppNetworkStatus) {

	key := status.Key()
	log.Infof("publishAppNetworkStatus(%s-%s)\n", status.DisplayName, key)
	pub := ctx.pubAppNetworkStatus
	pub.Publish(key, status)
}

func publishNetworkObjectStatus(ctx *zedrouterContext,
	status *types.NetworkObjectStatus) {
	key := status.Key()
	log.Debugf("publishNetworkObjectStatus(%s)\n", key)
	pub := ctx.pubNetworkObjectStatus
	pub.Publish(key, *status)
}

func unpublishAppNetworkStatus(ctx *zedrouterContext,
	status *types.AppNetworkStatus) {

	key := status.Key()
	log.Debugf("unpublishAppNetworkStatus(%s)\n", key)
	pub := ctx.pubAppNetworkStatus
	st, _ := pub.Get(key)
	if st == nil {
		log.Errorf("unpublishAppNetworkStatus(%s) not found\n", key)
		return
	}
	pub.Unpublish(key)
}

func unpublishNetworkObjectStatus(ctx *zedrouterContext,
	status *types.NetworkObjectStatus) {
	key := status.Key()
	log.Debugf("unpublishNetworkObjectStatus(%s)\n", key)
	pub := ctx.pubNetworkObjectStatus
	st, _ := pub.Get(key)
	if st == nil {
		log.Errorf("unpublishNetworkObjectStatus(%s) not found\n", key)
		return
	}
	pub.Unpublish(key)
}

func unpublishLispDataplaneConfig(ctx *zedrouterContext,
	status *types.LispDataplaneConfig) {
	key := "global"
	log.Debugf("unpublishLispDataplaneConfig(%s)\n", key)
	pub := ctx.pubLispDataplaneConfig
	st, _ := pub.Get(key)
	if st == nil {
		log.Errorf("unpublishLispDataplaneConfig(%s) not found\n", key)
		return
	}
	pub.Unpublish(key)
}

// Format a json string with any additional info
func generateAdditionalInfo(status types.AppNetworkStatus, olConfig types.OverlayNetworkConfig) string {
	additionalInfo := ""
	if status.IsZedmanager {
		if olConfig.AdditionalInfoDevice != nil {
			b, err := json.Marshal(olConfig.AdditionalInfoDevice)
			if err != nil {
				log.Fatal(err, "json Marshal AdditionalInfoDevice")
			}
			additionalInfo = string(b)
			log.Debugf("Generated additional info device %s\n",
				additionalInfo)
		}
	} else {
		// Combine subset of the device and application information
		addInfoApp := types.AdditionalInfoApp{
			DeviceEID:   deviceEID,
			DeviceIID:   deviceIID,
			DisplayName: status.DisplayName,
		}
		if additionalInfoDevice != nil {
			addInfoApp.UnderlayIP = additionalInfoDevice.UnderlayIP
			addInfoApp.Hostname = additionalInfoDevice.Hostname
		}
		b, err := json.Marshal(addInfoApp)
		if err != nil {
			log.Fatal(err, "json Marshal AdditionalInfoApp")
		}
		additionalInfo = string(b)
		log.Debugf("Generated additional info app %s\n",
			additionalInfo)
	}
	return additionalInfo
}

func updateLispConfiglets(ctx *zedrouterContext, legacyDataPlane bool) {
	pub := ctx.pubAppNetworkStatus
	items := pub.GetAll()
	for _, st := range items {
		status := cast.CastAppNetworkStatus(st)
		for i, olStatus := range status.OverlayNetworkList {
			olNum := i + 1
			var olIfname string
			var IID uint32
			if status.IsZedmanager {
				olIfname = "dbo" + strconv.Itoa(olNum) + "x" +
					strconv.Itoa(status.AppNum)
				IID = olStatus.MgmtIID
			} else {
				olIfname = olStatus.Bridge
				// Need to get the IID from the service
				serviceStatus := lookupAppLink(ctx, olStatus.Network)
				if serviceStatus == nil {
					log.Errorf("updateLispConfiglets: Network %s is not attached to any service\n",
						olStatus.Network.String())
					continue
				}
				if serviceStatus.Activated == false {
					log.Errorf("updateLispConfiglets: Network service %s not activated\n",
						serviceStatus.Key())
					continue
				}
				IID = serviceStatus.LispStatus.IID
			}
			additionalInfo := generateAdditionalInfo(status,
				olStatus.OverlayNetworkConfig)
			log.Debugf("updateLispConfiglets for %s isMgmt %v IID %d\n",
				olIfname, status.IsZedmanager, IID)
			createLispConfiglet(lispRunDirname, status.IsZedmanager,
				IID, olStatus.EID,
				olStatus.AppIPAddr, olStatus.LispSignature,
				*ctx.deviceNetworkStatus, olIfname,
				olIfname, additionalInfo,
				olStatus.MgmtMapServers, legacyDataPlane)
		}
	}
}

// Wrappers around handleCreate, handleModify, and handleDelete

// Determine whether it is an create or modify
func handleAppNetworkConfigModify(ctxArg interface{}, key string, configArg interface{}) {

	ctx := ctxArg.(*zedrouterContext)
	config := cast.CastAppNetworkConfig(configArg)
	log.Infof("handleAppNetworkConfigModify(%s-%s)\n", config.DisplayName, key)

	status := lookupAppNetworkStatus(ctx, key)
	if status == nil {
		handleAppNetworkCreate(ctx, key, config)
	} else {
		doAppNetworkConfigModify(ctx, key, config, status)
	}
	log.Infof("handleAppNetworkConfigModify(%s) done\n", key)
}

func handleAppNetworkConfigDelete(ctxArg interface{}, key string,
	configArg interface{}) {

	log.Infof("handleAppNetworkConfigDelete(%s)\n", key)
	ctx := ctxArg.(*zedrouterContext)
	status := lookupAppNetworkStatus(ctx, key)
	if status == nil {
		log.Infof("handleAppNetworkConfigDelete: unknown %s\n", key)
		return
	}
	handleDelete(ctx, key, status)
	log.Infof("handleAppNetworkConfigDelete(%s) done\n", key)
}

// This function separates Lisp service info/status into separate
// LispInfoStatus messages based on IID. This function also publishes the separated
// lisp info/status messages to correspoinding NetworkServiceStatus objects.
func parseAndPublishLispServiceInfo(ctx *zedrouterContext, lispInfo *types.LispInfoStatus) {
	// map for splitting the status info per IID
	infoMap := make(map[uint64]*types.LispInfoStatus)

	// Separate lispInfo into multiple LispInfoStatus structure based on IID
	for _, dbMap := range lispInfo.DatabaseMaps {
		iid := dbMap.IID

		// check we have entry for this iid in our infoMap
		var infoEntry *types.LispInfoStatus
		var ok bool
		infoEntry, ok = infoMap[iid]
		if !ok {
			infoEntry = &types.LispInfoStatus{}
			infoEntry.ItrCryptoPort = lispInfo.ItrCryptoPort
			infoEntry.EtrNatPort = lispInfo.EtrNatPort
			infoEntry.Interfaces = lispInfo.Interfaces
			infoEntry.DecapKeys = lispInfo.DecapKeys
			infoMap[iid] = infoEntry
		}
		infoEntry.DatabaseMaps = append(infoEntry.DatabaseMaps, dbMap)
	}

	// Update LispStatus in service instance status based on it's IID
	pub := ctx.pubNetworkServiceStatus
	stList := pub.GetAll()
	// IID to service status map for Lisp service instances
	stMap := make(map[uint64]types.NetworkServiceStatus)
	for _, st := range stList {
		status := cast.CastNetworkServiceStatus(st)
		if status.Type != types.NST_LISP {
			continue
		}
		serviceIID := uint64(status.LispStatus.IID)
		stMap[serviceIID] = status
	}

	for iid, lispStatus := range infoMap {
		status, ok := stMap[iid]
		if !ok {
			continue
		}
		if cmp.Equal(status.LispStatus, lispStatus) {
			continue
		} else {
			log.Debugf("parseAndPublishLispServiceInfo: Publish diff %s to zedcloud\n",
				cmp.Diff(status.LispStatus, lispStatus))
		}
		status.LispInfoStatus = lispStatus

		// publish the changes
		publishNetworkServiceStatus(ctx, &status, true)
	}
}

// This function separates Lisp service info/status into separate
// LispInfoStatus messages based on IID. This function also publishes the separated
// lisp info/status messages to correspoinding NetworkInstanceStatus objects.
func parseAndPublishLispInstanceInfo(ctx *zedrouterContext, lispInfo *types.LispInfoStatus) {
	// map for splitting the status info per IID
	infoMap := make(map[uint64]*types.LispInfoStatus)

	// Separate lispInfo into multiple LispInfoStatus structure based on IID
	for _, dbMap := range lispInfo.DatabaseMaps {
		iid := dbMap.IID

		// check we have entry for this iid in our infoMap
		var infoEntry *types.LispInfoStatus
		var ok bool
		infoEntry, ok = infoMap[iid]
		if !ok {
			infoEntry = &types.LispInfoStatus{}
			infoEntry.ItrCryptoPort = lispInfo.ItrCryptoPort
			infoEntry.EtrNatPort = lispInfo.EtrNatPort
			infoEntry.Interfaces = lispInfo.Interfaces
			infoEntry.DecapKeys = lispInfo.DecapKeys
			infoMap[iid] = infoEntry
		}
		infoEntry.DatabaseMaps = append(infoEntry.DatabaseMaps, dbMap)
	}

	// Update LispStatus in service instance status based on it's IID
	pub := ctx.pubNetworkInstanceStatus
	stList := pub.GetAll()
	// IID to instance status map for Lisp network instances
	stMap := make(map[uint64]types.NetworkInstanceStatus)
	for _, st := range stList {
		status := cast.CastNetworkInstanceStatus(st)
		if status.Type != types.NetworkInstanceTypeMesh {
			continue
		}
		iid := uint64(status.LispStatus.IID)
		stMap[iid] = status
	}

	for iid, lispStatus := range infoMap {
		status, ok := stMap[iid]
		if !ok {
			continue
		}
		if cmp.Equal(status.LispStatus, lispStatus) {
			continue
		} else {
			log.Debugf("parseAndPublishLispInstanceInfo: Publish diff %s to zedcloud\n",
				cmp.Diff(status.LispStatus, lispStatus))
		}
		status.LispInfoStatus = lispStatus

		// publish the changes
		publishNetworkInstanceStatus(ctx, &status)
	}
}

func handleLispInfoModify(ctxArg interface{}, key string, configArg interface{}) {
	log.Infof("handleLispInfoModify(%s)\n", key)
	ctx := ctxArg.(*zedrouterContext)
	lispInfo := cast.CastLispInfoStatus(configArg)

	if key != "global" {
		log.Infof("handleLispInfoModify: ignoring %s\n", key)
		return
	}
	// XXX remove the Service one
	parseAndPublishLispServiceInfo(ctx, &lispInfo)
	parseAndPublishLispInstanceInfo(ctx, &lispInfo)
	log.Infof("handleLispInfoModify(%s) done\n", key)
}

func handleLispInfoDelete(ctxArg interface{}, key string, configArg interface{}) {
	// XXX No-op.
	log.Infof("handleLispInfoDelete(%s)\n", key)
	log.Infof("handleLispInfoDelete(%s) done\n", key)
}

// This function separates Lisp metrics by IID. lisp-ztr dataplane sends statistics
// of all IIDs together. We need to separate statistics per IID and associate them to
// the NetworkServiceStatus to which a given IID (statistics) belong.
// This function also publishes the Lisp metrics to NetworkServiceStatus.
func parseAndPublishLispMetricsOLD(ctx *zedrouterContext, lispMetrics *types.LispMetrics) {
	// map for splitting the metrics per IID
	metricMap := make(map[uint64]*types.LispMetrics)

	// Separate lispMetrics into multiple LispMetrics based on IID
	for _, dbMap := range lispMetrics.EidStats {
		iid := dbMap.IID

		// check we have entry for this iid in our metricMap
		var metricEntry *types.LispMetrics
		var ok bool
		metricEntry, ok = metricMap[iid]
		if !ok {
			metricEntry = &types.LispMetrics{}
			// Copy global statistics. We do it by directly
			// assigning structures.
			*metricEntry = *lispMetrics
			metricEntry.EidStats = []types.EidStatistics{}
			metricEntry.EidMaps = []types.EidMap{}
			metricMap[iid] = metricEntry
		}
		metricEntry.EidStats = append(metricEntry.EidStats, dbMap)
	}

	// Copy IID to Eid maps to relevant IID LispMetric structurs
	for _, eidMap := range lispMetrics.EidMaps {
		iid := eidMap.IID
		metricEntry, ok := metricMap[iid]
		if ok {
			metricEntry.EidMaps = append(metricEntry.EidMaps, eidMap)
		}
	}

	// Update Lisp metrics in service instance status based on it's IID
	pub := ctx.pubNetworkServiceStatus
	stList := pub.GetAll()
	// IID to service status map for Lisp service instances
	stMap := make(map[uint64]types.NetworkServiceStatus)
	for _, st := range stList {
		status := cast.CastNetworkServiceStatus(st)
		if status.Type != types.NST_LISP {
			continue
		}
		serviceIID := uint64(status.LispStatus.IID)
		stMap[serviceIID] = status
	}

	// Populate the metrics that we have in NetworkServiceMetrics objects
	// of corresponding IID.
	// We loop through NetworkServiceStatus objects to find the services
	// that are currently active on device and then populate metrics in their
	// respective NetworkServiceMetrics structures and publish them.
	for iid, metrics := range metricMap {
		status, ok := stMap[iid]
		if !ok {
			continue
		}
		metricsStatus := lookupNetworkServiceMetrics(ctx, status.Key())
		if metricsStatus == nil {
			metricsStatus = new(types.NetworkServiceMetrics)
			if metricsStatus == nil {
				continue
			}
			metricsStatus.UUID = status.UUID
			metricsStatus.DisplayName = status.DisplayName
			metricsStatus.Type = status.Type
		}
		if (metricsStatus.LispMetrics != nil) &&
			cmp.Equal(metricsStatus.LispMetrics, metrics) {
			continue
		} else {
			log.Debugf("parseAndPublishLispMetrics: Publish diff %s to zedcloud\n",
				cmp.Diff(metricsStatus.LispMetrics, metrics))
		}
		metricsStatus.LispMetrics = metrics

		// publish the changes
		publishNetworkServiceMetrics(ctx, metricsStatus, true)
	}
}

// This function separates Lisp metrics by IID. lisp-ztr dataplane sends statistics
// of all IIDs together. We need to separate statistics per IID and associate them to
// the NetworkServiceStatus to which a given IID (statistics) belong.
// This function also publishes the Lisp metrics to NetworkInstanceStatus.
func parseAndPublishLispMetrics(ctx *zedrouterContext, lispMetrics *types.LispMetrics) {
	// map for splitting the metrics per IID
	metricMap := make(map[uint64]*types.LispMetrics)

	// Separate lispMetrics into multiple LispMetrics based on IID
	for _, dbMap := range lispMetrics.EidStats {
		iid := dbMap.IID

		// check we have entry for this iid in our metricMap
		var metricEntry *types.LispMetrics
		var ok bool
		metricEntry, ok = metricMap[iid]
		if !ok {
			metricEntry = &types.LispMetrics{}
			// Copy global statistics. We do it by directly
			// assigning structures.
			*metricEntry = *lispMetrics
			metricEntry.EidStats = []types.EidStatistics{}
			metricEntry.EidMaps = []types.EidMap{}
			metricMap[iid] = metricEntry
		}
		metricEntry.EidStats = append(metricEntry.EidStats, dbMap)
	}

	// Copy IID to Eid maps to relevant IID LispMetric structurs
	for _, eidMap := range lispMetrics.EidMaps {
		iid := eidMap.IID
		metricEntry, ok := metricMap[iid]
		if ok {
			metricEntry.EidMaps = append(metricEntry.EidMaps, eidMap)
		}
	}

	// Update Lisp metrics in network instance status based on it's IID
	pub := ctx.pubNetworkInstanceStatus
	stList := pub.GetAll()
	// IID to service status map for Lisp service instances
	stMap := make(map[uint64]types.NetworkInstanceStatus)
	for _, st := range stList {
		status := cast.CastNetworkInstanceStatus(st)
		if status.Type != types.NetworkInstanceTypeMesh {
			continue
		}
		serviceIID := uint64(status.LispStatus.IID)
		stMap[serviceIID] = status
	}

	// Populate the metrics that we have in NetworkInstanceMetrics objects
	// of corresponding IID.
	// We loop through NetworkInstanceStatus objects to find the services
	// that are currently active on device and then populate metrics in their
	// respective NetworkInstanceMetrics structures and publish them.
	for iid, metrics := range metricMap {
		status, ok := stMap[iid]
		if !ok {
			continue
		}
		metricsStatus := lookupNetworkInstanceMetrics(ctx, status.Key())
		if metricsStatus == nil {
			metricsStatus = new(types.NetworkInstanceMetrics)
			if metricsStatus == nil {
				continue
			}
			metricsStatus.UUIDandVersion = status.UUIDandVersion
			metricsStatus.DisplayName = status.DisplayName
			metricsStatus.Type = status.Type
		}
		if (metricsStatus.LispMetrics != nil) &&
			cmp.Equal(metricsStatus.LispMetrics, metrics) {
			continue
		} else {
			log.Debugf("parseAndPublishLispMetrics: Publish diff %s to zedcloud\n",
				cmp.Diff(metricsStatus.LispMetrics, metrics))
		}
		metricsStatus.LispMetrics = metrics

		// publish the changes
		publishNetworkInstanceMetrics(ctx, metricsStatus)
	}
}

func handleLispMetricsModify(ctxArg interface{}, key string, configArg interface{}) {
	log.Debugf("handleLispMetricsModify(%s)\n", key)
	ctx := ctxArg.(*zedrouterContext)
	lispMetrics := cast.CastLispMetrics(configArg)

	if key != "global" {
		log.Infof("handleLispMetricsModify: ignoring %s\n", key)
		return
	}
	// XXX remove the OLD service one
	parseAndPublishLispMetricsOLD(ctx, &lispMetrics)
	parseAndPublishLispMetrics(ctx, &lispMetrics)
	log.Debugf("handleLispMetricsModify(%s) done\n", key)
}

func handleLispMetricsDelete(ctxArg interface{}, key string, configArg interface{}) {
	// No-op
	log.Infof("handleLispMetricsDelete(%s)\n", key)
	log.Infof("handleLispMetricsDelete(%s) done\n", key)
}

// Callers must be careful to publish any changes to AppNetworkStatus
func lookupAppNetworkStatus(ctx *zedrouterContext, key string) *types.AppNetworkStatus {

	pub := ctx.pubAppNetworkStatus
	st, _ := pub.Get(key)
	if st == nil {
		log.Infof("lookupAppNetworkStatus(%s) not found\n", key)
		return nil
	}
	status := cast.CastAppNetworkStatus(st)
	if status.Key() != key {
		log.Errorf("lookupAppNetworkStatus key/UUID mismatch %s vs %s; ignored %+v\n",
			key, status.Key(), status)
		return nil
	}
	return &status
}

func lookupAppNetworkConfig(ctx *zedrouterContext, key string) *types.AppNetworkConfig {

	sub := ctx.subAppNetworkConfig
	c, _ := sub.Get(key)
	if c == nil {
		sub = ctx.subAppNetworkConfigAg
		c, _ = sub.Get(key)
		if c == nil {
			log.Infof("lookupAppNetworkConfig(%s) not found\n", key)
			return nil
		}
	}
	config := cast.CastAppNetworkConfig(c)
	if config.Key() != key {
		log.Errorf("lookupAppNetworkConfig key/UUID mismatch %s vs %s; ignored %+v\n",
			key, config.Key(), config)
		return nil
	}
	return &config
}

// Track the device information so we can annotate the application EIDs
// Note that when we start with zedrouter config files in place the
// device one might be processed after application ones, in which case these
// empty. This results in less additional info recorded in the map servers.
// XXX note that this only works well when the IsZedmanager AppNetworkConfig
// arrives first so that these fields are filled in before other
// AppNetworkConfig entries are processed.
var deviceEID net.IP
var deviceIID uint32
var additionalInfoDevice *types.AdditionalInfoDevice

func handleAppNetworkCreate(ctx *zedrouterContext, key string,
	config types.AppNetworkConfig) {
	log.Infof("handleAppAppNetworkCreate(%v) for %s\n",
		config.UUIDandVersion, config.DisplayName)

	// Pick a local number to identify the application instance
	// Used for IP addresses as well bridge and file names.
	appNum := appNumAllocate(ctx, config.UUIDandVersion.UUID,
		config.IsZedmanager)

	// Start by marking with PendingAdd
	status := types.AppNetworkStatus{
		UUIDandVersion: config.UUIDandVersion,
		AppNum:         appNum,
		PendingAdd:     true,
		DisplayName:    config.DisplayName,
		IsZedmanager:   config.IsZedmanager,
	}
	publishAppNetworkStatus(ctx, &status)

	if config.Activate {
		doActivate(ctx, config, &status)
	}
	status.PendingAdd = false
	publishAppNetworkStatus(ctx, &status)
	log.Infof("handleCreateAppNetwork done for %s\n", config.DisplayName)
}

func doActivate(ctx *zedrouterContext, config types.AppNetworkConfig,
	status *types.AppNetworkStatus) {

	log.Infof("%s-%s: IsZedmanager:%t\n",
		config.DisplayName, config.UUIDandVersion, config.IsZedmanager)

	if config.IsZedmanager {
		doActivateAppInstanceWithMgmtLisp(ctx, config, status)
		return
	}

	// Check that Network exists for all overlays and underlays.
	// We look for MissingNetwork when a NetworkObject is added
	allNetworksExist := appNetworkCheckAllNetworksExist(ctx, config, status)
	if !allNetworksExist {
		// XXX error or not?
		status.MissingNetwork = true
		log.Infof("doActivate(%v) for %s: missing networks\n",
			config.UUIDandVersion, config.DisplayName)
		publishAppNetworkStatus(ctx, status)
		return
	}
	appNetworkDoCopyNetworksToStatus(config, status)

	// Note that with IPv4/IPv6/LISP interfaces the domU can do
	// dns lookups on either IPv4 and IPv6 on any interface, hence we
	// configure the ipsets for all the domU's interfaces/bridges.
	ipsets := compileAppInstanceIpsets(ctx, config.OverlayNetworkList,
		config.UnderlayNetworkList)

	appNetworkDoActivateOverlayNetworks(ctx, config, status, ipsets)

	appNetworkDoActivateAllUnderlayNetworks(ctx, config, status, ipsets)

	status.Activated = true
	publishAppNetworkStatus(ctx, status)
	log.Infof("doActivate done for %s\n", config.DisplayName)
}

func appNetworkDoActivateAllUnderlayNetworks(
	ctx *zedrouterContext,
	config types.AppNetworkConfig,
	status *types.AppNetworkStatus,
	ipsets []string) {
	for i, ulConfig := range config.UnderlayNetworkList {
		ulNum := i + 1
		log.Debugf("ulNum %d network %s ACLs %v\n",
			ulNum, ulConfig.Network.String(), ulConfig.ACLs)
		if ulConfig.UsesNetworkInstance {
			appNetworkDoActivateUnderlayNetworkWithNetworkInstance(
				ctx, config, status, ipsets, &ulConfig, ulNum)
		} else {
			appNetworkDoActivateUnderlayNetworkWithNetworkObject(
				ctx, config, status, ipsets, &ulConfig, ulNum)
		}

	}
}

// Get Switch's IPv4 address for the port in NetworkInstance
func getSwitchIPv4Addr(ctx *zedrouterContext,
	status *types.NetworkInstanceStatus) (string, error) {
	// Find any service which is associated with the appLink UUID
	log.Infof("getSwitchIPv4Addr(%s-%s)\n",
		status.DisplayName, status.UUID.String())
	if status.Type != types.NetworkInstanceTypeSwitch {
		errStr := fmt.Sprintf("NI not a switch. Type: %d", status.Type)
		return "", errors.New(errStr)
	}
	if status.Port == "" {
		log.Infof("SwitchType, but no Adapter\n")
		return "", nil
	}

	ifname := types.AdapterToIfName(ctx.deviceNetworkStatus, status.Port)
	ifindex, err := devicenetwork.IfnameToIndex(ifname)
	if err != nil {
		errStr := fmt.Sprintf("getSwitchIPv4Addr(%s): IfnameToIndex(%s) failed %s",
			status.DisplayName, ifname, err)
		return "", errors.New(errStr)
	}
	addrs, err := devicenetwork.IfindexToAddrs(ifindex)
	if err != nil {
		errStr := fmt.Sprintf("getSwitchIPv4Addr(%s): IfindexToAddrs(%s, index %d) failed %s",
			status.DisplayName, ifname, ifindex, err)
		return "", errors.New(errStr)
	}
	for _, addr := range addrs {
		log.Infof("getSwitchIPv4Addr(%s): found addr %s\n",
			status.DisplayName, addr.IP.String())
		// XXX Add IPv6 underlay; ignore link-locals.
		if addr.IP.To4() != nil {
			return addr.IP.String(), nil
		}
	}
	log.Infof("getSwitchIPv4Addr(%s): no IPv4 address on %s yet\n",
		status.DisplayName, status.Port)
	return "", nil
}

func appNetworkDoActivateUnderlayNetworkWithNetworkInstance(
	ctx *zedrouterContext,
	config types.AppNetworkConfig,
	status *types.AppNetworkStatus,
	ipsets []string,
	ulConfig *types.UnderlayNetworkConfig,
	ulNum int) {

	netInstConfig := lookupNetworkInstanceConfig(ctx,
		ulConfig.Network.String())
	if netInstConfig == nil {
		log.Fatalf("Cannot find UL NetworkObject %s for App %s",
			ulConfig.Name, config.DisplayName)
	}
	netInstStatus := lookupNetworkInstanceStatus(ctx,
		ulConfig.Network.String())
	if netInstStatus == nil {
		errStr := fmt.Sprintf("no status for %s",
			ulConfig.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "doActivate underlay", err)
		return
	}
	if netInstStatus.Error != "" {
		log.Errorf("doActivate sees network error %s\n",
			netInstStatus.Error)
		addError(ctx, status, "error from network instance",
			errors.New(netInstStatus.Error))
		return
	}
	networkInstanceInfo := &netInstStatus.NetworkInstanceInfo

	// Fetch the network that this underlay is attached to
	bridgeName := networkInstanceInfo.BridgeName
	vifName := "nbu" + strconv.Itoa(ulNum) + "x" +
		strconv.Itoa(status.AppNum)
	uLink, err := findBridge(bridgeName)
	if err != nil {
		addError(ctx, status, "findBridge", err)
		log.Infof("doActivate done for %s\n",
			config.DisplayName)
		return
	}
	bridgeMac := uLink.HardwareAddr
	log.Infof("bridgeName %s MAC %s\n",
		bridgeName, bridgeMac.String())

	var appMac string // Handed to domU
	if ulConfig.AppMacAddr != nil {
		appMac = ulConfig.AppMacAddr.String()
	} else {
		// Room to handle multiple underlays in 5th byte
		appMac = fmt.Sprintf("00:16:3e:00:%02x:%02x",
			ulNum, status.AppNum)
	}
	log.Infof("appMac %s\n", appMac)

	// Record what we have so far
	ulStatus := &status.UnderlayNetworkList[ulNum-1]
	log.Infof("doActivate ulNum %d: %v\n", ulNum, ulStatus)
	ulStatus.Name = ulConfig.Name
	ulStatus.Bridge = bridgeName
	ulStatus.BridgeMac = bridgeMac
	ulStatus.Vif = vifName
	ulStatus.Mac = appMac
	ulStatus.HostName = config.Key()

	bridgeIPAddr, appIPAddr := getUlAddrsForNetworkInstance(ctx, ulNum-1,
		status.AppNum, ulStatus, netInstStatus)

	// Check if we have a bridge service with an address
	bridgeIP, err := getSwitchIPv4Addr(ctx, netInstStatus)
	if err != nil {
		log.Infof("doActivate: %s\n", err)
	} else if bridgeIP != "" {
		log.Infof("bridgeIp: %s\n", bridgeIP)
		bridgeIPAddr = bridgeIP
	}
	log.Infof("bridgeIPAddr %s appIPAddr %s\n", bridgeIPAddr, appIPAddr)
	ulStatus.BridgeIPAddr = bridgeIPAddr
	// XXX appIPAddr is "" if bridge service
	ulStatus.AssignedIPAddr = appIPAddr
	hostsDirpath := runDirname + "/hosts." + bridgeName
	if appIPAddr != "" {
		addToHostsConfiglet(hostsDirpath, config.DisplayName,
			[]string{appIPAddr})
	}

	// Default ipset
	deleteDefaultIpsetConfiglet(vifName, false)
	createDefaultIpsetConfiglet(vifName, netInstStatus.DnsNameToIPList,
		appIPAddr)

	// Set up ACLs
	err = createACLConfiglet(bridgeName, vifName, false,
		ulConfig.ACLs, bridgeIPAddr, appIPAddr)
	if err != nil {
		addError(ctx, status, "createACL", err)
	}

	if appIPAddr != "" {
		// XXX clobber any IPv6 EID entry since same name
		// but that's probably OK since we're doing IPv4 EIDs
		addhostDnsmasq(bridgeName, appMac, appIPAddr,
			config.UUIDandVersion.UUID.String())
	}

	// Look for added or deleted ipsets
	newIpsets, staleIpsets, restartDnsmasq := diffIpsets(ipsets,
		networkInstanceInfo.BridgeIPSets)

	if restartDnsmasq && ulStatus.BridgeIPAddr != "" {
		stopDnsmasq(bridgeName, true, false)
		createDnsmasqConfigletForNetworkInstance(bridgeName,
			ulStatus.BridgeIPAddr, netInstConfig, hostsDirpath,
			newIpsets, false)
		startDnsmasq(bridgeName)
	}
	networkInstanceInfo.AddVif(vifName, appMac,
		config.UUIDandVersion.UUID)
	networkInstanceInfo.BridgeIPSets = newIpsets
	log.Infof("set BridgeIPSets to %v for %s", newIpsets,
		networkInstanceInfo.BridgeName)
	publishNetworkInstanceStatus(ctx, netInstStatus)

	maybeRemoveStaleIpsets(staleIpsets)
}

func appNetworkDoActivateUnderlayNetworkWithNetworkObject(
	ctx *zedrouterContext,
	config types.AppNetworkConfig,
	status *types.AppNetworkStatus,
	ipsets []string,
	ulConfig *types.UnderlayNetworkConfig,
	ulNum int) {

	netconfig := lookupNetworkObjectConfig(ctx,
		ulConfig.Network.String())
	if netconfig == nil {
		log.Fatalf("Cannot find UL NetworkObject %s for App %s",
			ulConfig.Name, config.DisplayName)
	}
	netstatus := lookupNetworkObjectStatus(ctx,
		ulConfig.Network.String())
	if netstatus == nil {
		errStr := fmt.Sprintf("no status for %s",
			ulConfig.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "doActivate underlay", err)
		return
	}
	if netstatus.Error != "" {
		log.Errorf("doActivate sees network error %s\n",
			netstatus.Error)
		addError(ctx, status, "netstatus.Error",
			errors.New(netstatus.Error))
		return
	}
	networkInstanceInfo := &netstatus.NetworkInstanceInfo

	// Fetch the network that this underlay is attached to
	bridgeName := networkInstanceInfo.BridgeName
	vifName := "nbu" + strconv.Itoa(ulNum) + "x" +
		strconv.Itoa(status.AppNum)
	uLink, err := findBridge(bridgeName)
	if err != nil {
		addError(ctx, status, "findBridge", err)
		log.Infof("doActivate done for %s\n",
			config.DisplayName)
		return
	}
	bridgeMac := uLink.HardwareAddr
	log.Infof("bridgeName %s MAC %s\n",
		bridgeName, bridgeMac.String())

	var appMac string // Handed to domU
	if ulConfig.AppMacAddr != nil {
		appMac = ulConfig.AppMacAddr.String()
	} else {
		// Room to handle multiple underlays in 5th byte
		appMac = fmt.Sprintf("00:16:3e:00:%02x:%02x",
			ulNum, status.AppNum)
	}
	log.Infof("appMac %s\n", appMac)

	// Record what we have so far
	ulStatus := &status.UnderlayNetworkList[ulNum-1]
	log.Infof("doActivate ulNum %d: %v\n", ulNum, ulStatus)
	ulStatus.Name = ulConfig.Name
	ulStatus.Bridge = bridgeName
	ulStatus.BridgeMac = bridgeMac
	ulStatus.Vif = vifName
	ulStatus.Mac = appMac
	ulStatus.HostName = config.Key()

	bridgeIPAddr, appIPAddr := getUlAddrs(ctx, ulNum-1,
		status.AppNum, ulStatus, netstatus)
	// Check if we have a bridge service with an address
	bridgeIP, err := getBridgeServiceIPv4Addr(ctx, ulConfig.Network)
	if err != nil {
		log.Infof("doActivate: %s\n", err)
	} else if bridgeIP != "" {
		bridgeIPAddr = bridgeIP
	}
	log.Infof("bridgeIPAddr %s appIPAddr %s\n", bridgeIPAddr, appIPAddr)
	ulStatus.BridgeIPAddr = bridgeIPAddr
	// XXX appIPAddr is "" if bridge service
	ulStatus.AssignedIPAddr = appIPAddr
	hostsDirpath := runDirname + "/hosts." + bridgeName
	if appIPAddr != "" {
		addToHostsConfiglet(hostsDirpath, config.DisplayName,
			[]string{appIPAddr})
	}

	// Default ipset
	deleteDefaultIpsetConfiglet(vifName, false)
	createDefaultIpsetConfiglet(vifName, netstatus.DnsNameToIPList,
		appIPAddr)

	// Set up ACLs
	err = createACLConfiglet(bridgeName, vifName, false,
		ulConfig.ACLs, bridgeIPAddr, appIPAddr)
	if err != nil {
		addError(ctx, status, "createACL", err)
	}

	if appIPAddr != "" {
		// XXX clobber any IPv6 EID entry since same name
		// but that's probably OK since we're doing IPv4 EIDs
		addhostDnsmasq(bridgeName, appMac, appIPAddr,
			config.UUIDandVersion.UUID.String())
	}

	// Look for added or deleted ipsets
	newIpsets, staleIpsets, restartDnsmasq := diffIpsets(ipsets,
		networkInstanceInfo.BridgeIPSets)

	if restartDnsmasq && ulStatus.BridgeIPAddr != "" {
		stopDnsmasq(bridgeName, true, false)
		createDnsmasqConfiglet(bridgeName,
			ulStatus.BridgeIPAddr, netconfig, hostsDirpath,
			newIpsets, false)
		startDnsmasq(bridgeName)
	}
	networkInstanceInfo.AddVif(vifName, appMac,
		config.UUIDandVersion.UUID)
	networkInstanceInfo.BridgeIPSets = newIpsets
	log.Infof("set BridgeIPSets to %v for %s", newIpsets,
		networkInstanceInfo.BridgeName)
	publishNetworkObjectStatus(ctx, netstatus)

	maybeRemoveStaleIpsets(staleIpsets)
}

func appNetworkDoActivateOverlayNetworks(
	ctx *zedrouterContext,
	config types.AppNetworkConfig,
	status *types.AppNetworkStatus,
	ipsets []string) {
	log.Infof("appNetworkDoActivateOverlayNetworks():")

	// XXX - The errors in this function should either be Fatal
	//	or abort activating the App Instance. We activate
	//	the App Instance only if all the networks are available
	//	and there are no errors.
	//	Cleanup this code to be consistent in the error handling.
	for i, olConfig := range config.OverlayNetworkList {
		olNum := i + 1
		log.Debugf("olNum %d network %s ACLs %v\n",
			olNum, olConfig.Network.String(), olConfig.ACLs)
		if olConfig.UsesNetworkInstance {
			appNetworkDoActivateOverlayNetworkWithNetworkInstance(ctx,
				config, status, ipsets, olConfig, olNum)
		} else {
			appNetworkDoActivateOverlayNetworkWithNetworkObject(ctx,
				config, status, ipsets, olConfig, olNum)
		}
	}
}

func appNetworkDoActivateOverlayNetworkWithNetworkInstance(
	ctx *zedrouterContext, config types.AppNetworkConfig,
	status *types.AppNetworkStatus, ipsets []string,
	olConfig types.OverlayNetworkConfig, olNum int) {

	netInstConfig := lookupNetworkInstanceConfig(ctx,
		olConfig.Network.String())
	if netInstConfig == nil {
		log.Fatalf("Cannot find OL NetworkInstanceConfig %s for App %s",
			olConfig.Name, config.DisplayName)
	}

	// Fetch the network that this overlay is attached to
	netInstStatus := lookupNetworkInstanceStatus(ctx,
		olConfig.Network.String())
	if netInstStatus == nil {
		// We had a netconfig but no status!
		errStr := fmt.Sprintf("no network status for %s",
			olConfig.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "handlecreate overlay", err)
		return
	}
	if netInstStatus.Error != "" {
		log.Errorf("doActivate sees network error %s\n",
			netInstStatus.Error)
		addError(ctx, status, "netstatus.Error",
			errors.New(netInstStatus.Error))
		return
	}
	bridgeNum := netInstStatus.BridgeNum
	bridgeName := netInstStatus.BridgeName
	vifName := "nbo" + strconv.Itoa(olNum) + "x" +
		strconv.Itoa(bridgeNum)

	oLink, err := findBridge(bridgeName)
	if err != nil {
		addError(ctx, status, "findBridge", err)
		log.Infof("doActivate done for %s\n",
			config.DisplayName)
		return
	}
	bridgeMac := oLink.HardwareAddr
	log.Infof("bridgeName %s MAC %s\n",
		bridgeName, bridgeMac.String())

	var appMac string // Handed to domU
	if olConfig.AppMacAddr != nil {
		appMac = olConfig.AppMacAddr.String()
	} else {
		appMac = fmt.Sprintf("00:16:3e:01:%02x:%02x",
			olNum, status.AppNum)
	}
	log.Infof("appMac %s\n", appMac)

	// Record what we have so far
	olStatus := &status.OverlayNetworkList[olNum-1]
	log.Infof("doActivate olNum %d: %v\n", olNum, olStatus)
	olStatus.Name = olConfig.Name
	olStatus.Bridge = bridgeName
	olStatus.BridgeMac = bridgeMac
	olStatus.Vif = vifName
	olStatus.Mac = appMac
	olStatus.HostName = config.Key()

	// BridgeIPAddr is set when network is up.
	olStatus.BridgeIPAddr = netInstStatus.BridgeIPAddr
	log.Infof("bridgeIPAddr %s\n", olStatus.BridgeIPAddr)

	// Create a host route towards the domU EID
	EID := olConfig.AppIPAddr
	isIPv6 := (EID.To4() == nil)

	// Consistency check
	if isIPv6 != !netInstStatus.Ipv4Eid {
		var errStr string
		if isIPv6 {
			errStr = fmt.Sprintf("IPv4 EID network %s and no IPv4 EID",
				olConfig.Network.String())
		} else {
			errStr = fmt.Sprintf("IPv6 EID network %s with an IPv4 EID %s",
				olConfig.Network.String(),
				olConfig.AppIPAddr.String())
		}
		addError(ctx, status, "doActivate",
			errors.New(errStr))
		log.Infof("doActivate done for %s\n",
			config.DisplayName)
		return
	}
	var subnetSuffix string
	if isIPv6 {
		subnetSuffix = "/128"
	} else {
		subnetSuffix = "/32"
	}
	//    ip -6 route add ${EID}/128 dev ${bridgeName}
	// or
	//    ip route add ${EID}/32 dev ${bridgeName}
	_, ipnet, err := net.ParseCIDR(EID.String() + subnetSuffix)
	if err != nil {
		errStr := fmt.Sprintf("ParseCIDR %s failed: %v",
			EID.String()+subnetSuffix, err)
		addError(ctx, status, "doActivate",
			errors.New(errStr))
		log.Infof("doActivate done for %s\n",
			config.DisplayName)
		return
	}
	rt := netlink.Route{Dst: ipnet, LinkIndex: oLink.Index}
	if err := netlink.RouteAdd(&rt); err != nil {
		errStr := fmt.Sprintf("RouteAdd %s failed: %s",
			EID, err)
		addError(ctx, status, "doActivate",
			errors.New(errStr))
		log.Infof("doActivate done for %s\n",
			config.DisplayName)
		return
	}

	// Write our EID hostname in a separate file in directory to
	// facilitate adds and deletes
	hostsDirpath := runDirname + "/hosts." + bridgeName
	addToHostsConfiglet(hostsDirpath, config.DisplayName,
		[]string{EID.String()})

	// Default ipset
	deleteDefaultIpsetConfiglet(vifName, false)
	createDefaultIpsetConfiglet(vifName, netInstStatus.DnsNameToIPList,
		EID.String())

	// Set up ACLs
	err = createACLConfiglet(bridgeName, vifName, false,
		olConfig.ACLs, olStatus.BridgeIPAddr, EID.String())
	if err != nil {
		addError(ctx, status, "createACL", err)
	}

	addhostDnsmasq(bridgeName, appMac, EID.String(),
		config.UUIDandVersion.UUID.String())

	// Look for added or deleted ipsets
	newIpsets, staleIpsets, restartDnsmasq := diffIpsets(ipsets,
		netInstStatus.BridgeIPSets)

	if restartDnsmasq && olStatus.BridgeIPAddr != "" {
		stopDnsmasq(bridgeName, true, false)
		createDnsmasqConfigletForNetworkInstance(bridgeName,
			olStatus.BridgeIPAddr, netInstConfig, hostsDirpath,
			newIpsets, netInstStatus.Ipv4Eid)
		startDnsmasq(bridgeName)
	}
	netInstStatus.AddVif(vifName, appMac,
		config.UUIDandVersion.UUID)
	netInstStatus.BridgeIPSets = newIpsets
	log.Infof("set BridgeIPSets to %v for %s", newIpsets, netInstStatus.Key())
	publishNetworkInstanceStatus(ctx, netInstStatus)

	maybeRemoveStaleIpsets(staleIpsets)

	createAndStartLispWithNetworkInstance(ctx, *status, olConfig,
		netInstStatus, lispRunDirname, bridgeName)
}

func appNetworkDoActivateOverlayNetworkWithNetworkObject(
	ctx *zedrouterContext, config types.AppNetworkConfig,
	status *types.AppNetworkStatus, ipsets []string,
	olConfig types.OverlayNetworkConfig, olNum int) {

	netconfig := lookupNetworkObjectConfig(ctx,
		olConfig.Network.String())
	if netconfig == nil {
		log.Fatalf("Cannot find OL NetworkObject %s for App %s",
			olConfig.Name, config.DisplayName)
	}

	// Fetch the network that this overlay is attached to
	netstatus := lookupNetworkObjectStatus(ctx,
		olConfig.Network.String())
	if netstatus == nil {
		// We had a netconfig but no status!
		errStr := fmt.Sprintf("no network status for %s",
			olConfig.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "handlecreate overlay", err)
		return
	}
	if netstatus.Error != "" {
		log.Errorf("doActivate sees network error %s\n",
			netstatus.Error)
		addError(ctx, status, "netstatus.Error",
			errors.New(netstatus.Error))
		return
	}
	bridgeNum := netstatus.BridgeNum
	bridgeName := netstatus.BridgeName
	vifName := "nbo" + strconv.Itoa(olNum) + "x" +
		strconv.Itoa(bridgeNum)

	oLink, err := findBridge(bridgeName)
	if err != nil {
		addError(ctx, status, "findBridge", err)
		log.Infof("doActivate done for %s\n",
			config.DisplayName)
		return
	}
	bridgeMac := oLink.HardwareAddr
	log.Infof("bridgeName %s MAC %s\n",
		bridgeName, bridgeMac.String())

	var appMac string // Handed to domU
	if olConfig.AppMacAddr != nil {
		appMac = olConfig.AppMacAddr.String()
	} else {
		appMac = fmt.Sprintf("00:16:3e:01:%02x:%02x",
			olNum, status.AppNum)
	}
	log.Infof("appMac %s\n", appMac)

	// Record what we have so far
	olStatus := &status.OverlayNetworkList[olNum-1]
	log.Infof("doActivate olNum %d: %v\n", olNum, olStatus)
	olStatus.Name = olConfig.Name
	olStatus.Bridge = bridgeName
	olStatus.BridgeMac = bridgeMac
	olStatus.Vif = vifName
	olStatus.Mac = appMac
	olStatus.HostName = config.Key()

	// BridgeIPAddr is set when network is up.
	olStatus.BridgeIPAddr = netstatus.BridgeIPAddr
	log.Infof("bridgeIPAddr %s\n", olStatus.BridgeIPAddr)

	// Create a host route towards the domU EID
	EID := olConfig.AppIPAddr
	isIPv6 := (EID.To4() == nil)

	// Consistency check
	if isIPv6 != !netstatus.Ipv4Eid {
		var errStr string
		if isIPv6 {
			errStr = fmt.Sprintf("IPv4 EID network %s and no IPv4 EID",
				olConfig.Network.String())
		} else {
			errStr = fmt.Sprintf("IPv6 EID network %s with an IPv4 EID %s",
				olConfig.Network.String(),
				olConfig.AppIPAddr.String())
		}
		addError(ctx, status, "doActivate",
			errors.New(errStr))
		log.Infof("doActivate done for %s\n",
			config.DisplayName)
		return
	}

	var subnetSuffix string
	if isIPv6 {
		subnetSuffix = "/128"
	} else {
		subnetSuffix = "/32"
	}
	//    ip -6 route add ${EID}/128 dev ${bridgeName}
	// or
	//    ip route add ${EID}/32 dev ${bridgeName}
	_, ipnet, err := net.ParseCIDR(EID.String() + subnetSuffix)
	if err != nil {
		errStr := fmt.Sprintf("ParseCIDR %s failed: %v",
			EID.String()+subnetSuffix, err)
		addError(ctx, status, "doActivate",
			errors.New(errStr))
		log.Infof("doActivate done for %s\n",
			config.DisplayName)
		return
	}
	rt := netlink.Route{Dst: ipnet, LinkIndex: oLink.Index}
	if err := netlink.RouteAdd(&rt); err != nil {
		errStr := fmt.Sprintf("RouteAdd %s failed: %s",
			EID, err)
		addError(ctx, status, "doActivate",
			errors.New(errStr))
		log.Infof("doActivate done for %s\n",
			config.DisplayName)
		return
	}

	// Write our EID hostname in a separate file in directory to
	// facilitate adds and deletes
	hostsDirpath := runDirname + "/hosts." + bridgeName
	addToHostsConfiglet(hostsDirpath, config.DisplayName,
		[]string{EID.String()})

	// Default ipset
	deleteDefaultIpsetConfiglet(vifName, false)
	createDefaultIpsetConfiglet(vifName, netstatus.DnsNameToIPList,
		EID.String())

	// Set up ACLs
	err = createACLConfiglet(bridgeName, vifName, false,
		olConfig.ACLs, olStatus.BridgeIPAddr, EID.String())
	if err != nil {
		addError(ctx, status, "createACL", err)
	}

	addhostDnsmasq(bridgeName, appMac, EID.String(),
		config.UUIDandVersion.UUID.String())

	// Look for added or deleted ipsets
	newIpsets, staleIpsets, restartDnsmasq := diffIpsets(ipsets,
		netstatus.BridgeIPSets)

	if restartDnsmasq && olStatus.BridgeIPAddr != "" {
		stopDnsmasq(bridgeName, true, false)
		createDnsmasqConfiglet(bridgeName,
			olStatus.BridgeIPAddr, netconfig, hostsDirpath,
			newIpsets, netstatus.Ipv4Eid)
		startDnsmasq(bridgeName)
	}
	netstatus.AddVif(vifName, appMac,
		config.UUIDandVersion.UUID)
	netstatus.BridgeIPSets = newIpsets
	log.Infof("set BridgeIPSets to %v for %s", newIpsets, netstatus.Key())
	publishNetworkObjectStatus(ctx, netstatus)

	maybeRemoveStaleIpsets(staleIpsets)

	// Create LISP configlets for IID and EID/signature
	serviceStatus := lookupAppLink(ctx, olConfig.Network)
	if serviceStatus == nil {
		// Lisp service might not have arrived as part of configuration.
		// Bail now and let the service activation take care of creating
		// Lisp configlets and re-start lispers.net
		log.Infof("doActivate: Network %s is not attached to any service\n",
			olConfig.Network.String())
		return
	}
	if serviceStatus.Activated == false {
		// Lisp service is not activate yet. Let the Lisp service activation
		// code take care of creating the Lisp configlets.
		log.Infof("doActivate: Network service %s not activated\n",
			serviceStatus.Key())
		return
	}

	createAndStartLisp(ctx, *status, olConfig,
		serviceStatus, lispRunDirname, bridgeName)
}

func appNetworkDoCopyNetworksToStatus(
	config types.AppNetworkConfig,
	status *types.AppNetworkStatus) {

	olcount := len(config.OverlayNetworkList)
	if olcount > 0 {
		log.Infof("Received olcount %d\n", olcount)
	}
	// XXX are we clobbering? Create in handleCreate?
	// XXX can count change for inactivate/activate?
	status.OverlayNetworkList = make([]types.OverlayNetworkStatus,
		olcount)
	for i := range config.OverlayNetworkList {
		status.OverlayNetworkList[i].OverlayNetworkConfig =
			config.OverlayNetworkList[i]
	}
	ulcount := len(config.UnderlayNetworkList)
	status.UnderlayNetworkList = make([]types.UnderlayNetworkStatus,
		ulcount)
	for i := range config.UnderlayNetworkList {
		status.UnderlayNetworkList[i].UnderlayNetworkConfig =
			config.UnderlayNetworkList[i]
	}
}

func appNetworkCheckAllNetworksExist(
	ctx *zedrouterContext,
	config types.AppNetworkConfig,
	status *types.AppNetworkStatus) bool {

	// Check networks for OL
	for _, olConfig := range config.OverlayNetworkList {
		if olConfig.UsesNetworkInstance {
			netInstConfig := lookupNetworkInstanceConfig(ctx,
				olConfig.Network.String())
			if netInstConfig != nil {
				continue
			}
		} else {
			netconfig := lookupNetworkObjectConfig(ctx,
				olConfig.Network.String())
			if netconfig != nil {
				continue
			}
		}
		// Neither NetworkObjectStatus Nor NetworkInstanceConfig found.
		// XXX no olStatus yet!
		errStr := fmt.Sprintf("Missing overlay network %s for %s/%s",
			olConfig.Network.String(),
			config.UUIDandVersion, config.DisplayName)
		log.Infof("doActivate failed: %s\n", errStr)
		addError(ctx, status, "doActivate overlay",
			errors.New(errStr))
		return false
	}

	// Check networks for Underlay
	// XXX - Should we also check for Network(instance)Status
	// objects here itself?
	for _, ulConfig := range config.UnderlayNetworkList {
		if ulConfig.UsesNetworkInstance {
			netInstConfig := lookupNetworkInstanceConfig(ctx,
				ulConfig.Network.String())
			if netInstConfig != nil {
				continue
			}
		} else {
			netconfig := lookupNetworkObjectConfig(ctx,
				ulConfig.Network.String())
			if netconfig != nil {
				continue
			}
		}
		// Neither NetworkObjectStatus Nor NetworkInstanceConfig found.
		// XXX no ulStatus yet!
		errStr := fmt.Sprintf("Missing underlay network %s for %s/%s",
			ulConfig.Network.String(),
			config.UUIDandVersion, config.DisplayName)
		log.Infof("doActivate failed: %s\n", errStr)
		addError(ctx, status, "doActivate underlay",
			errors.New(errStr))
		return false
	}
	return true
}

func doActivateAppInstanceWithMgmtLisp(
	ctx *zedrouterContext,
	config types.AppNetworkConfig,
	status *types.AppNetworkStatus) {
	log.Infof("doActivate: for %s IsZedmanager\n",
		config.DisplayName)
	if len(config.OverlayNetworkList) != 1 ||
		len(config.UnderlayNetworkList) != 0 {
		// XXX report IsZedmanager error to cloud?
		err := errors.New("Malformed IsZedmanager config; ignored")
		addError(ctx, status, "IsZedmanager", err)
		log.Infof("doActivate done for %s\n",
			config.DisplayName)
		return
	}
	ctx.legacyDataPlane = config.LegacyDataPlane
	dataplaneConfig := types.LispDataplaneConfig{
		Legacy: ctx.legacyDataPlane,
	}
	publishLispDataplaneConfig(ctx, &dataplaneConfig)

	// Use this olIfname to name files
	// XXX some files might not be used until Zedmanager becomes
	// a domU at which point IsZedMansger boolean won't be needed
	olConfig := config.OverlayNetworkList[0]
	olNum := 1
	olIfname := "dbo" + strconv.Itoa(olNum) + "x" +
		strconv.Itoa(status.AppNum)
	// Assume there is no UUID for management overlay

	// Create olIfname dummy interface with EID and fd00::/8 route
	// pointing at it.
	// XXX also a separate route for eidAllocationPrefix if global

	// Start clean
	attrs := netlink.NewLinkAttrs()
	attrs.Name = olIfname
	oLink := &netlink.Dummy{LinkAttrs: attrs}
	netlink.LinkDel(oLink)

	//    ip link add ${olIfname} type dummy
	attrs = netlink.NewLinkAttrs()
	attrs.Name = olIfname
	// Note: we ignore olConfig.AppMacAddr for IsMgmt
	olIfMac := fmt.Sprintf("00:16:3e:02:%02x:%02x", olNum,
		status.AppNum)
	hw, err := net.ParseMAC(olIfMac)
	if err != nil {
		log.Fatal("ParseMAC failed: ", olIfMac, err)
	}
	attrs.HardwareAddr = hw
	oLink = &netlink.Dummy{LinkAttrs: attrs}
	if err := netlink.LinkAdd(oLink); err != nil {
		errStr := fmt.Sprintf("LinkAdd on %s failed: %s",
			olIfname, err)
		addError(ctx, status, "IsZedmanager",
			errors.New(errStr))
	}

	// ip link set ${olIfname} mtu 1280
	if err := netlink.LinkSetMTU(oLink, 1280); err != nil {
		errStr := fmt.Sprintf("LinkSetMTU on %s failed: %s",
			olIfname, err)
		addError(ctx, status, "IsZedmanager",
			errors.New(errStr))
	}

	//    ip link set ${olIfname} up
	if err := netlink.LinkSetUp(oLink); err != nil {
		errStr := fmt.Sprintf("LinkSetUp on %s failed: %s",
			olIfname, err)
		addError(ctx, status, "IsZedmanager",
			errors.New(errStr))
	}

	//    ip link set ${olIfname} arp on
	if err := netlink.LinkSetARPOn(oLink); err != nil {
		errStr := fmt.Sprintf("LinkSetARPOn on %s failed: %s",
			olIfname, err)
		addError(ctx, status, "IsZedmanager",
			errors.New(errStr))
	}

	// Configure the EID on olIfname and set up a default route
	// for all fd00 EIDs
	//    ip addr add ${EID}/128 dev ${olIfname}
	EID := olConfig.EID
	addr, err := netlink.ParseAddr(EID.String() + "/128")
	if err != nil {
		errStr := fmt.Sprintf("ParseAddr %s failed: %s",
			EID, err)
		addError(ctx, status, "IsZedmanager",
			errors.New(errStr))
		log.Infof("doActivate done for %s\n",
			config.DisplayName)
		return
	}
	if err := netlink.AddrAdd(oLink, addr); err != nil {
		errStr := fmt.Sprintf("AddrAdd %s failed: %s", EID, err)
		addError(ctx, status, "IsZedmanager",
			errors.New(errStr))
	}

	//    ip route add fd00::/8 via fe80::1 dev $intf
	index := oLink.Attrs().Index
	_, ipnet, err := net.ParseCIDR("fd00::/8")
	if err != nil {
		log.Fatal("ParseCIDR fd00::/8 failed:\n", err)
	}
	via := net.ParseIP("fe80::1")
	if via == nil {
		log.Fatal("ParseIP fe80::1 failed: ", err)
	}
	// Need to do both an add and a change since we could have
	// a FAILED neighbor entry from a previous run and a down
	// interface.
	//    ip nei add fe80::1 lladdr 00:16:3e:02:01:00 dev $intf
	//    ip nei change fe80::1 lladdr 00:16:3e:02:01:00 dev $intf
	neigh := netlink.Neigh{LinkIndex: index, IP: via,
		HardwareAddr: hw, State: netlink.NUD_PERMANENT}
	if err := netlink.NeighAdd(&neigh); err != nil {
		errStr := fmt.Sprintf("NeighAdd fe80::1 failed: %s",
			err)
		addError(ctx, status, "IsZedmanager",
			errors.New(errStr))
	}
	if err := netlink.NeighSet(&neigh); err != nil {
		errStr := fmt.Sprintf("NeighSet fe80::1 failed: %s",
			err)
		addError(ctx, status, "IsZedmanager",
			errors.New(errStr))
	}

	rt := netlink.Route{Dst: ipnet, LinkIndex: index, Gw: via}
	if err := netlink.RouteAdd(&rt); err != nil {
		errStr := fmt.Sprintf("RouteAdd fd00::/8 failed: %s",
			err)
		addError(ctx, status, "IsZedmanager",
			errors.New(errStr))
	}

	// XXX NOTE: this hosts file is not read!
	// XXX easier when Zedmanager is in separate domU!
	// Create a hosts file for the overlay based on DnsNameToIPList
	// Directory is /var/run/zedrouter/hosts.${OLIFNAME}
	// Each hostname in a separate file in directory to facilitate
	// adds and deletes
	hostsDirpath := runDirname + "/hosts." + olIfname
	deleteHostsConfiglet(hostsDirpath, false)
	createHostsConfiglet(hostsDirpath, olConfig.MgmtDnsNameToIPList)

	// Default ipset
	deleteDefaultIpsetConfiglet(olIfname, false)
	createDefaultIpsetConfiglet(olIfname, olConfig.MgmtDnsNameToIPList,
		EID.String())

	// Set up ACLs
	err = createACLConfiglet(olIfname, olIfname, true, olConfig.ACLs,
		"", "")
	if err != nil {
		addError(ctx, status, "createACL", err)
	}

	// Save information about zedmanger EID and additional info
	deviceEID = EID
	deviceIID = olConfig.MgmtIID
	additionalInfoDevice = olConfig.AdditionalInfoDevice

	additionalInfo := generateAdditionalInfo(*status, olConfig)

	// Create LISP configlets for IID and EID/signature
	createLispConfiglet(lispRunDirname, config.IsZedmanager,
		olConfig.MgmtIID, olConfig.EID, nil,
		olConfig.LispSignature,
		*ctx.deviceNetworkStatus, olIfname, olIfname,
		additionalInfo, olConfig.MgmtMapServers,
		ctx.legacyDataPlane)
	status.OverlayNetworkList = make([]types.OverlayNetworkStatus,
		len(config.OverlayNetworkList))
	for i := range config.OverlayNetworkList {
		status.OverlayNetworkList[i].OverlayNetworkConfig =
			config.OverlayNetworkList[i]
		// XXX set BridgeName, BridgeIPAddr?
	}
	status.Activated = true
	publishAppNetworkStatus(ctx, status)
	log.Infof("doActivate done for %s\n", config.DisplayName)
}

// Called when a NetworkObject is added
// Walk all AppNetworkStatus looking for MissingNetwork, then
// check if network UUID is there.
func checkAndRecreateAppNetwork(
	ctx *zedrouterContext, network uuid.UUID) {

	log.Infof("checkAndRecreateAppNetwork(%s)\n", network.String())
	pub := ctx.pubAppNetworkStatus
	items := pub.GetAll()
	for _, st := range items {
		status := cast.CastAppNetworkStatus(st)
		if !status.MissingNetwork {
			continue
		}
		log.Infof("checkAndRecreateAppNetwork(%s) missing for %s\n",
			network.String(), status.DisplayName)

		if status.IsZedmanager {
			continue
		}
		config := lookupAppNetworkConfig(ctx, status.Key())
		if config == nil {
			log.Warnf("checkAndRecreateAppNetwork(%s) no config for %s\n",
				network.String(), status.DisplayName)
			continue
		}
		if !config.IsNetworkUsed(network) {
			continue
		}
		log.Infof("checkAndRecreateAppNetwork(%s) recreating for %s\n",
			network.String(), status.DisplayName)
		if status.Error != "" {
			log.Infof("checkAndRecreateAppNetwork(%s) remove error %s for %s\n",
				network.String(), status.Error,
				status.DisplayName)
			status.Error = ""
			status.ErrorTime = time.Time{}
		}
		doActivate(ctx, *config, &status)
		log.Infof("checkAndRecreateAppNetwork done for %s\n",
			config.DisplayName)
	}
}
func createAndStartLispWithNetworkInstance(ctx *zedrouterContext,
	status types.AppNetworkStatus,
	olConfig types.OverlayNetworkConfig,
	instStatus *types.NetworkInstanceStatus,
	lispRunDirname, bridgeName string) {

	additionalInfo := generateAdditionalInfo(status, olConfig)
	ifnames := adapterToIfNames(ctx, instStatus.Port)
	ifnameMap := make(map[string]bool)
	for _, adapter := range ifnames {
		ifnameMap[adapter] = true
	}
	deviceNetworkParams := types.DeviceNetworkStatus{}
	for _, port := range ctx.deviceNetworkStatus.Ports {
		if _, ok := ifnameMap[port.IfName]; ok == true {
			deviceNetworkParams.Ports =
				append(deviceNetworkParams.Ports, port)
		}
	}
	createLispEidConfiglet(lispRunDirname, instStatus.LispConfig.IID,
		olConfig.EID, olConfig.AppIPAddr, olConfig.LispSignature,
		deviceNetworkParams, bridgeName, bridgeName, additionalInfo,
		instStatus.LispConfig.MapServers, ctx.legacyDataPlane)
}

func createAndStartLisp(ctx *zedrouterContext,
	status types.AppNetworkStatus,
	olConfig types.OverlayNetworkConfig,
	serviceStatus *types.NetworkServiceStatus,
	lispRunDirname, bridgeName string) {

	if serviceStatus == nil {
		log.Infof("createAndStartLisp: No service configured yet\n")
		return
	}

	additionalInfo := generateAdditionalInfo(status, olConfig)
	ifnames := adapterToIfNames(ctx, serviceStatus.Adapter)
	ifnameMap := make(map[string]bool)
	for _, adapter := range ifnames {
		ifnameMap[adapter] = true
	}
	deviceNetworkParams := types.DeviceNetworkStatus{}
	for _, port := range ctx.deviceNetworkStatus.Ports {
		if _, ok := ifnameMap[port.IfName]; ok == true {
			deviceNetworkParams.Ports =
				append(deviceNetworkParams.Ports, port)
		}
	}
	createLispEidConfiglet(lispRunDirname, serviceStatus.LispStatus.IID,
		olConfig.EID, olConfig.AppIPAddr, olConfig.LispSignature,
		deviceNetworkParams, bridgeName, bridgeName, additionalInfo,
		serviceStatus.LispStatus.MapServers, ctx.legacyDataPlane)
}

// Returns the link
func findBridge(bridgeName string) (*netlink.Bridge, error) {

	var bridgeLink *netlink.Bridge
	link, err := netlink.LinkByName(bridgeName)
	if link == nil {
		errStr := fmt.Sprintf("findBridge(%s) failed %s",
			bridgeName, err)
		// XXX how to handle this failure? bridge disappeared?
		return nil, errors.New(errStr)
	}
	switch link.(type) {
	case *netlink.Bridge:
		bridgeLink = link.(*netlink.Bridge)
	default:
		errStr := fmt.Sprintf("findBridge(%s) not a bridge %T",
			bridgeName, link)
		// XXX why wouldn't it be a bridge?
		return nil, errors.New(errStr)
	}
	return bridgeLink, nil
}

// XXX Need additional logic for IPv6 underlays.
func getUlAddrs(ctx *zedrouterContext, ifnum int, appNum int,
	status *types.UnderlayNetworkStatus,
	netstatus *types.NetworkObjectStatus) (string, string) {

	log.Infof("getUlAddrs(%d/%d)\n", ifnum, appNum)

	bridgeIPAddr := ""
	appIPAddr := ""

	// Allocate bridgeIPAddr based on BridgeMac
	log.Infof("getUlAddrs(%d/%d for %s) bridgeMac %s\n",
		ifnum, appNum, netstatus.UUID.String(),
		status.BridgeMac.String())
	addr, err := lookupOrAllocateIPv4(ctx, netstatus,
		status.BridgeMac)
	if err != nil {
		log.Errorf("lookupOrAllocatePv4 failed %s\n", err)
	} else {
		bridgeIPAddr = addr
	}

	if status.AppIPAddr != nil {
		// Static IP assignment case.
		// Note that appIPAddr can be in a different subnet.
		// Assumption is that the config specifies a gateway/router
		// in the same subnet as the static address.
		appIPAddr = status.AppIPAddr.String()
	} else if status.Mac != "" {
		// XXX or change type of VifInfo.Mac to avoid parsing?
		mac, err := net.ParseMAC(status.Mac)
		if err != nil {
			log.Fatal("ParseMAC failed: ", status.Mac, err)
		}
		log.Infof("getUlAddrs(%d/%d for %s) app Mac %s\n",
			ifnum, appNum, netstatus.UUID.String(), mac.String())
		addr, err := lookupOrAllocateIPv4(ctx, netstatus, mac)
		if err != nil {
			log.Errorf("lookupOrAllocateIPv4 failed %s\n", err)
		} else {
			appIPAddr = addr
		}
	}
	log.Infof("getUlAddrs(%d/%d) done %s/%s\n",
		ifnum, appNum, bridgeIPAddr, appIPAddr)
	return bridgeIPAddr, appIPAddr
}

// XXX Need additional logic for IPv6 underlays.
func getUlAddrsForNetworkInstance(ctx *zedrouterContext,
	ifnum int, appNum int,
	status *types.UnderlayNetworkStatus,
	netInstStatus *types.NetworkInstanceStatus) (string, string) {

	log.Infof("getUlAddrs(%d/%d)\n", ifnum, appNum)

	bridgeIPAddr := ""
	appIPAddr := ""

	// Allocate bridgeIPAddr based on BridgeMac
	log.Infof("getUlAddrs(%d/%d for %s) bridgeMac %s\n",
		ifnum, appNum, netInstStatus.UUID.String(),
		status.BridgeMac.String())
	addr, err := lookupOrAllocateIPv4ForNetworkInstance(ctx, netInstStatus,
		status.BridgeMac)
	if err != nil {
		log.Errorf("lookupOrAllocatePv4 failed %s\n", err)
	} else {
		bridgeIPAddr = addr
	}

	if status.AppIPAddr != nil {
		// Static IP assignment case.
		// Note that appIPAddr can be in a different subnet.
		// Assumption is that the config specifies a gateway/router
		// in the same subnet as the static address.
		appIPAddr = status.AppIPAddr.String()
	} else if status.Mac != "" {
		// XXX or change type of VifInfo.Mac to avoid parsing?
		mac, err := net.ParseMAC(status.Mac)
		if err != nil {
			log.Fatal("ParseMAC failed: ", status.Mac, err)
		}
		log.Infof("getUlAddrs(%d/%d for %s) app Mac %s\n",
			ifnum, appNum, netInstStatus.UUID.String(), mac.String())
		addr, err := lookupOrAllocateIPv4ForNetworkInstance(ctx, netInstStatus, mac)
		if err != nil {
			log.Errorf("lookupOrAllocateIPv4 failed %s\n", err)
		} else {
			appIPAddr = addr
		}
	}
	log.Infof("getUlAddrs(%d/%d) done %s/%s\n",
		ifnum, appNum, bridgeIPAddr, appIPAddr)
	return bridgeIPAddr, appIPAddr
}

// Caller should clear the appropriate status.Pending* if the the caller will
// return after adding the error.
func addError(ctx *zedrouterContext,
	status *types.AppNetworkStatus, tag string, err error) {

	log.Infof("%s: %s\n", tag, err.Error())
	status.Error = appendError(status.Error, tag, err.Error())
	status.ErrorTime = time.Now()
	publishAppNetworkStatus(ctx, status)
}

func appendError(allErrors string, prefix string, lasterr string) string {
	return fmt.Sprintf("%s%s: %s\n\n", allErrors, prefix, lasterr)
}

// Note that handleModify will not touch the EID; just ACLs
// XXX should we check that nothing else has changed?
// XXX If so flag other changes as errors; would need lastError in status.
func doAppNetworkConfigModify(ctx *zedrouterContext, key string,
	config types.AppNetworkConfig, status *types.AppNetworkStatus) {

	log.Infof("handleModify(%v) for %s\n",
		config.UUIDandVersion, config.DisplayName)

	if !doAppNetworkSanityCheckForModify(ctx, config, status) {
		log.Errorf("handleModify: Config check failed for %s\n", config.DisplayName)
		return
	}

	// No check for version numbers since the ACLs etc might change
	// even for the same version.
	log.Debugf("handleModify appNum %d\n", status.AppNum)
	status.PendingModify = true

	// Check for unsupported changes
	status.LegacyDataPlane = ctx.legacyDataPlane
	status.UUIDandVersion = config.UUIDandVersion
	publishAppNetworkStatus(ctx, status)

	if !config.Activate && status.Activated {
		doInactivateAppNetwork(ctx, status)
	}

	if config.IsZedmanager {
		log.Debugf("ZedManager AppNetwork\n")
		handleAppNetworkWithMgmtLispModify(ctx, config, status)
		return
	}

	// Note that with IPv4/IPv6/LISP interfaces the domU can do
	// dns lookups on either IPv4 and IPv6 on any interface, hence should
	// configure the ipsets for all the domU's interfaces/bridges.
	ipsets := compileAppInstanceIpsets(ctx, config.OverlayNetworkList,
		config.UnderlayNetworkList)

	// Look for ACL changes in overlay
	doAppNetworkModifyAllOverlayNetworks(ctx, config, status, ipsets)

	// Look for ACL changes in underlay
	doAppNetworkModifyAllUnderlayNetworks(ctx, config, status, ipsets)

	// Write out what we modified to AppNetworkStatus
	// Note that lengths are the same as before
	for i := range config.OverlayNetworkList {
		status.OverlayNetworkList[i].OverlayNetworkConfig =
			config.OverlayNetworkList[i]
	}
	for i := range config.UnderlayNetworkList {
		status.UnderlayNetworkList[i].UnderlayNetworkConfig =
			config.UnderlayNetworkList[i]
	}

	if config.Activate && !status.Activated {
		// XXX the doAppNetworkModify calls above did
		// an updateACL and doActivate will do a createACL resulting
		// in duplicate (but harmless) rules.
		doActivate(ctx, config, status)
	}

	status.PendingModify = false
	publishAppNetworkStatus(ctx, status)
	log.Infof("handleModify done for %s\n", config.DisplayName)
}

func doAppNetworkSanityCheckForModify(ctx *zedrouterContext,
	config types.AppNetworkConfig, status *types.AppNetworkStatus) bool {
	if config.IsZedmanager != status.IsZedmanager {
		errStr := fmt.Sprintf("Unsupported: IsZedmanager changed for %s",
			config.UUIDandVersion)
		status.PendingModify = false
		addError(ctx, status, "handleModify", errors.New(errStr))
		log.Infof("handleModify done for %s\n", config.DisplayName)
		return false
	}
	if config.IsZedmanager {
		if config.LegacyDataPlane != ctx.legacyDataPlane {
			errStr := fmt.Sprintf("Unsupported: Changing legacy data plane flag on the fly\n")

			status.PendingModify = false
			addError(ctx, status, "handleModify",
				errors.New(errStr))
			log.Infof("handleModify done for %s\n",
				config.DisplayName)
			return false
		}
	}
	// XXX what about changing the number of interfaces as
	// part of an inactive/active transition?
	// XXX We could should we allow the addition of interfaces
	// if the domU would find out through some hotplug event.
	// But deletion is hard.
	// For now don't allow any adds or deletes.
	if len(config.OverlayNetworkList) != len(status.OverlayNetworkList) {
		errStr := fmt.Sprintf("Unsupported: Changed number of overlays for %s",
			config.UUIDandVersion)
		status.PendingModify = false
		addError(ctx, status, "handleModify", errors.New(errStr))
		log.Infof("handleModify done for %s\n", config.DisplayName)
		return false
	}
	if len(config.UnderlayNetworkList) != len(status.UnderlayNetworkList) {
		errStr := fmt.Sprintf("Unsupported: Changed number of underlays for %s",
			config.UUIDandVersion)
		status.PendingModify = false
		addError(ctx, status, "handleModify", errors.New(errStr))
		log.Infof("handleModify done for %s\n", config.DisplayName)
		return false
	}
	for i := range config.UnderlayNetworkList {
		ulConfig := &config.UnderlayNetworkList[i]
		if ulConfig.UsesNetworkInstance {
			netconfig := lookupNetworkInstanceConfig(ctx,
				ulConfig.Network.String())
			if netconfig == nil {
				errStr := fmt.Sprintf("no network Instance config for %s",
					ulConfig.Network.String())
				err := errors.New(errStr)
				addError(ctx, status, "lookupNetworkInstanceConfig", err)
				return false
			}
			netstatus := lookupNetworkInstanceStatus(ctx,
				ulConfig.Network.String())
			if netstatus == nil {
				// We had a netconfig but no status!
				errStr := fmt.Sprintf("no network Instance status for %s",
					ulConfig.Network.String())
				err := errors.New(errStr)
				addError(ctx, status, "handleModify underlay sanity check "+
					" - no network instance", err)
				return false
			}
		} else {
			netconfig := lookupNetworkObjectConfig(ctx,
				ulConfig.Network.String())
			if netconfig == nil {
				errStr := fmt.Sprintf("no network config for %s",
					ulConfig.Network.String())
				err := errors.New(errStr)
				addError(ctx, status, "lookupNetworkObjectConfig", err)
				return false
			}
			netstatus := lookupNetworkObjectStatus(ctx,
				ulConfig.Network.String())
			if netstatus == nil {
				// We had a netconfig but no status!
				errStr := fmt.Sprintf("no network status for %s",
					ulConfig.Network.String())
				err := errors.New(errStr)
				addError(ctx, status, "handleModify underlay", err)
				return false
			}
		}
	}
	return true

}

func doAppNetworkModifyAllUnderlayNetworks(
	ctx *zedrouterContext,
	config types.AppNetworkConfig,
	status *types.AppNetworkStatus,
	ipsets []string) {

	for i := range config.UnderlayNetworkList {
		log.Debugf("handleModify ulNum %d\n", i)
		ulConfig := &config.UnderlayNetworkList[i]
		ulStatus := &status.UnderlayNetworkList[i]
		if ulConfig.UsesNetworkInstance {
			doAppNetworkModifyUnderlayNetworkWithNetworkInstance(
				ctx, status, ulConfig, ulStatus, ipsets)
		} else {
			doAppNetworkModifyUnderlayNetworkWithNetworkObject(
				ctx, status, ulConfig, ulStatus, ipsets)
		}
	}
}

func doAppNetworkModifyUnderlayNetworkWithNetworkInstance(
	ctx *zedrouterContext,
	status *types.AppNetworkStatus,
	ulConfig *types.UnderlayNetworkConfig,
	ulStatus *types.UnderlayNetworkStatus,
	ipsets []string) {

	bridgeName := ulStatus.Bridge
	appIPAddr := ulStatus.AssignedIPAddr

	netconfig := lookupNetworkInstanceConfig(ctx, ulConfig.Network.String())
	netstatus := lookupNetworkInstanceStatus(ctx, ulConfig.Network.String())

	// We ignore any errors in netstatus

	// XXX could there be a change to AssignedIPAddress?
	// If so updateNetworkACLConfiglet needs to know old and new
	// XXX Could ulStatus.Vif not be set? Means we didn't add
	err := updateACLConfiglet(bridgeName, ulStatus.Vif, false,
		ulStatus.ACLs, ulConfig.ACLs, ulStatus.BridgeIPAddr,
		appIPAddr)
	if err != nil {
		addError(ctx, status, "updateACL", err)
	}

	newIpsets, staleIpsets, restartDnsmasq := diffIpsets(ipsets,
		netstatus.BridgeIPSets)

	if restartDnsmasq && ulStatus.BridgeIPAddr != "" {
		hostsDirpath := runDirname + "/hosts." + bridgeName
		stopDnsmasq(bridgeName, true, false)
		createDnsmasqConfigletForNetworkInstance(bridgeName,
			ulStatus.BridgeIPAddr, netconfig, hostsDirpath,
			newIpsets, false)
		startDnsmasq(bridgeName)
	}
	netstatus.RemoveVif(ulStatus.Vif)
	netstatus.BridgeIPSets = newIpsets
	log.Infof("set BridgeIPSets to %v for %s", newIpsets, netstatus.Key())
	publishNetworkInstanceStatus(ctx, netstatus)

	maybeRemoveStaleIpsets(staleIpsets)
}

func doAppNetworkModifyUnderlayNetworkWithNetworkObject(
	ctx *zedrouterContext,
	status *types.AppNetworkStatus,
	ulConfig *types.UnderlayNetworkConfig,
	ulStatus *types.UnderlayNetworkStatus,
	ipsets []string) {

	bridgeName := ulStatus.Bridge
	appIPAddr := ulStatus.AssignedIPAddr

	netconfig := lookupNetworkObjectConfig(ctx, ulConfig.Network.String())
	netstatus := lookupNetworkObjectStatus(ctx, ulConfig.Network.String())

	// We ignore any errors in netstatus

	// XXX could there be a change to AssignedIPAddress?
	// If so updateNetworkACLConfiglet needs to know old and new
	// XXX Could ulStatus.Vif not be set? Means we didn't add
	err := updateACLConfiglet(bridgeName, ulStatus.Vif, false,
		ulStatus.ACLs, ulConfig.ACLs, ulStatus.BridgeIPAddr,
		appIPAddr)
	if err != nil {
		addError(ctx, status, "updateACL", err)
	}

	newIpsets, staleIpsets, restartDnsmasq := diffIpsets(ipsets,
		netstatus.BridgeIPSets)

	if restartDnsmasq && ulStatus.BridgeIPAddr != "" {
		hostsDirpath := runDirname + "/hosts." + bridgeName
		stopDnsmasq(bridgeName, true, false)
		createDnsmasqConfiglet(bridgeName,
			ulStatus.BridgeIPAddr, netconfig, hostsDirpath,
			newIpsets, false)
		startDnsmasq(bridgeName)
	}
	netstatus.RemoveVif(ulStatus.Vif)
	netstatus.BridgeIPSets = newIpsets
	log.Infof("set BridgeIPSets to %v for %s", newIpsets, netstatus.Key())
	publishNetworkObjectStatus(ctx, netstatus)

	maybeRemoveStaleIpsets(staleIpsets)
}

func doAppNetworkModifyAllOverlayNetworks(
	ctx *zedrouterContext,
	config types.AppNetworkConfig,
	status *types.AppNetworkStatus,
	ipsets []string) {

	for i := range config.OverlayNetworkList {
		log.Debugf("handleModify olNum %d\n", i+1)
		olConfig := &config.OverlayNetworkList[i]
		olStatus := &status.OverlayNetworkList[i]
		if olConfig.UsesNetworkInstance {
			doAppNetworkModifyOverlayNetworkWithNetworkInstance(
				ctx, status, olConfig, olStatus, ipsets)
		} else {
			doAppNetworkModifyOverlayNetworkWithNetworkObject(
				ctx, status, olConfig, olStatus, ipsets)
		}

	}
}

func doAppNetworkModifyOverlayNetworkWithNetworkInstance(
	ctx *zedrouterContext,
	status *types.AppNetworkStatus,
	olConfig *types.OverlayNetworkConfig,
	olStatus *types.OverlayNetworkStatus,
	ipsets []string) {
	log.Infof("doAppNetworkModifyOverlayNetworkWithNetworkInstance():")
	bridgeName := olStatus.Bridge

	netconfig := lookupNetworkInstanceConfig(ctx,
		olConfig.Network.String())
	if netconfig == nil {
		errStr := fmt.Sprintf("no network config for %s",
			olConfig.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "doAppNetworkModifyOverlayNetworkWithNetworkInstance overlay", err)
		return
	}
	netstatus := lookupNetworkInstanceStatus(ctx,
		olConfig.Network.String())
	if netstatus == nil {
		// We had a netconfig but no status!
		errStr := fmt.Sprintf("no network status for %s",
			olConfig.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "doAppNetworkModifyOverlayNetworkWithNetworkInstance overlay",
			err)
		return
	}
	// We ignore any errors in netstatus

	// XXX could there be a change to AssignedIPv6Address aka EID?
	// If so updateACLConfiglet needs to know old and new
	// XXX Could olStatus.Vif not be set? Means we didn't add
	err := updateACLConfiglet(bridgeName, olStatus.Vif, false,
		olStatus.ACLs, olConfig.ACLs, olStatus.BridgeIPAddr,
		olConfig.EID.String())
	if err != nil {
		addError(ctx, status, "updateACL", err)
	}

	// Look for added or deleted ipsets
	newIpsets, staleIpsets, restartDnsmasq := diffIpsets(ipsets,
		netstatus.BridgeIPSets)

	if restartDnsmasq && olStatus.BridgeIPAddr != "" {
		hostsDirpath := runDirname + "/hosts." + bridgeName
		stopDnsmasq(bridgeName, true, false)
		createDnsmasqConfigletForNetworkInstance(bridgeName,
			olStatus.BridgeIPAddr, netconfig, hostsDirpath,
			newIpsets, netstatus.Ipv4Eid)
		startDnsmasq(bridgeName)
	}
	netstatus.NetworkInstanceInfo.RemoveVif(olStatus.Vif)
	netstatus.BridgeIPSets = newIpsets
	log.Infof("set BridgeIPSets to %v for %s", newIpsets, netstatus.Key())
	publishNetworkInstanceStatus(ctx, netstatus)

	maybeRemoveStaleIpsets(staleIpsets)

	additionalInfo := generateAdditionalInfo(*status, *olConfig)

	// Update any signature changes
	// XXX should we check that EID didn't change?

	// Create LISP configlets for IID and EID/signature
	updateLispConfiglet(lispRunDirname, false,
		netstatus.LispConfig.IID, olConfig.EID,
		olConfig.AppIPAddr, olConfig.LispSignature,
		*ctx.deviceNetworkStatus, bridgeName, bridgeName,
		additionalInfo, netstatus.LispConfig.MapServers,
		ctx.legacyDataPlane)
}

func doAppNetworkModifyOverlayNetworkWithNetworkObject(
	ctx *zedrouterContext,
	status *types.AppNetworkStatus,
	olConfig *types.OverlayNetworkConfig,
	olStatus *types.OverlayNetworkStatus,
	ipsets []string) {
	log.Infof("doAppNetworkModifyOverlayNetworkWithNetworkObject():")
	bridgeName := olStatus.Bridge

	netconfig := lookupNetworkObjectConfig(ctx,
		olConfig.Network.String())
	if netconfig == nil {
		errStr := fmt.Sprintf("no network config for %s",
			olConfig.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "doAppNetworkModifyOverlay", err)
		return
	}
	netstatus := lookupNetworkObjectStatus(ctx,
		olConfig.Network.String())
	if netstatus == nil {
		// We had a netconfig but no status!
		errStr := fmt.Sprintf("no network status for %s",
			olConfig.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "doAppNetworkModifyOverlayNetworkWithNetworkObject overlay",
			err)
		return
	}
	// We ignore any errors in netstatus

	// XXX could there be a change to AssignedIPv6Address aka EID?
	// If so updateACLConfiglet needs to know old and new
	// XXX Could olStatus.Vif not be set? Means we didn't add
	err := updateACLConfiglet(bridgeName, olStatus.Vif, false,
		olStatus.ACLs, olConfig.ACLs, olStatus.BridgeIPAddr,
		olConfig.EID.String())
	if err != nil {
		addError(ctx, status, "updateACL", err)
	}

	// Look for added or deleted ipsets
	newIpsets, staleIpsets, restartDnsmasq := diffIpsets(ipsets,
		netstatus.BridgeIPSets)

	if restartDnsmasq && olStatus.BridgeIPAddr != "" {
		hostsDirpath := runDirname + "/hosts." + bridgeName
		stopDnsmasq(bridgeName, true, false)
		createDnsmasqConfiglet(bridgeName,
			olStatus.BridgeIPAddr, netconfig, hostsDirpath,
			newIpsets, netstatus.Ipv4Eid)
		startDnsmasq(bridgeName)
	}
	netstatus.NetworkInstanceInfo.RemoveVif(olStatus.Vif)
	netstatus.BridgeIPSets = newIpsets
	log.Infof("set BridgeIPSets to %v for %s", newIpsets, netstatus.Key())
	publishNetworkObjectStatus(ctx, netstatus)

	maybeRemoveStaleIpsets(staleIpsets)

	serviceStatus := lookupAppLink(ctx, olConfig.Network)
	if serviceStatus == nil {
		// Lisp service might not have arrived as part of configuration.
		// Bail now and let the service activation take care of creating
		// Lisp configlets and re-start lispers.net
		return
	}

	additionalInfo := generateAdditionalInfo(*status, *olConfig)

	// Update any signature changes
	// XXX should we check that EID didn't change?

	// Create LISP configlets for IID and EID/signature
	updateLispConfiglet(lispRunDirname, false,
		serviceStatus.LispStatus.IID, olConfig.EID,
		olConfig.AppIPAddr, olConfig.LispSignature,
		*ctx.deviceNetworkStatus, bridgeName, bridgeName,
		additionalInfo, serviceStatus.LispStatus.MapServers,
		ctx.legacyDataPlane)
}

func handleAppNetworkWithMgmtLispModify(ctx *zedrouterContext,
	config types.AppNetworkConfig, status *types.AppNetworkStatus) {
	if !config.IsZedmanager {
		log.Fatalf("IsZedmanager false. Config: %+v\n", config)
	}
	olConfig := &config.OverlayNetworkList[0]
	olStatus := &status.OverlayNetworkList[0]
	olNum := 1
	olIfname := "dbo" + strconv.Itoa(olNum) + "x" +
		strconv.Itoa(status.AppNum)
	// Assume there is no UUID for management overlay

	// Note: we ignore olConfig.AppMacAddr for IsMgmt

	// Update ACLs
	err := updateACLConfiglet(olIfname, olIfname, true, olStatus.ACLs,
		olConfig.ACLs, "", "")
	if err != nil {
		addError(ctx, status, "updateACL", err)
	}

	if config.Activate && !status.Activated {
		doActivate(ctx, config, status)
	}
	status.PendingModify = false
	publishAppNetworkStatus(ctx, status)
	log.Infof("Mgmt List modify done for %s\n", config.DisplayName)
}

func maybeRemoveStaleIpsets(staleIpsets []string) {
	// Remove stale ipsets
	// In case if there are any references to these ipsets from other
	// domUs, then the kernel would not remove them.
	// The ipset destroy command would just fail.
	for _, ipset := range staleIpsets {
		err := ipsetDestroy(fmt.Sprintf("ipv4.%s", ipset))
		if err != nil {
			log.Errorln("ipset destroy ipv4", ipset, err)
		}
		err = ipsetDestroy(fmt.Sprintf("ipv6.%s", ipset))
		if err != nil {
			log.Errorln("ipset destroy ipv6", ipset, err)
		}
	}
}

func handleDelete(ctx *zedrouterContext, key string,
	status *types.AppNetworkStatus) {

	log.Infof("handleDelete(%v) for %s\n",
		status.UUIDandVersion, status.DisplayName)

	status.PendingDelete = true
	publishAppNetworkStatus(ctx, status)

	if status.Activated {
		doInactivateAppNetwork(ctx, status)
	}
	status.PendingDelete = false
	publishAppNetworkStatus(ctx, status)

	// Write out what we modified to AppNetworkStatus aka delete
	unpublishAppNetworkStatus(ctx, status)

	appNumFree(ctx, status.UUIDandVersion.UUID)
	log.Infof("handleDelete done for %s\n", status.DisplayName)
}

func doInactivateAppNetwork(ctx *zedrouterContext,
	status *types.AppNetworkStatus) {

	log.Infof("doInactivate(%v) for %s, IsZedManager:%t\n",
		status.UUIDandVersion, status.DisplayName, status.IsZedmanager)

	if status.IsZedmanager {
		doInactivateAppNetworkWithMgmtLisp(ctx, status)
		return
	}

	// Note that with IPv4/IPv6/LISP interfaces the domU can do
	// dns lookups on either IPv4 and IPv6 on any interface, hence should
	// configure the ipsets for all the domU's interfaces/bridges.
	// We skip our own contributions since we're going away
	ipsets := compileOldAppInstanceIpsets(ctx, status.OverlayNetworkList,
		status.UnderlayNetworkList, status.Key())

	// Delete everything for overlay
	appNetworkDoInactivateAllOverlayNetworks(ctx, status, ipsets)

	// Delete everything in underlay
	appNetworkDoInactivateAllUnderlayNetworks(ctx, status, ipsets)

	status.Activated = false
	publishAppNetworkStatus(ctx, status)
	log.Infof("doInactivate done for %s\n", status.DisplayName)
}

func appNetworkDoInactivateAllUnderlayNetworks(
	ctx *zedrouterContext,
	status *types.AppNetworkStatus,
	ipsets []string) {

	for ulNum := 0; ulNum < len(status.UnderlayNetworkList); ulNum++ {
		ulStatus := &status.UnderlayNetworkList[ulNum]
		log.Infof("doInactivate ulNum %d: %v\n", ulNum, ulStatus)
		if ulStatus.UsesNetworkInstance {
			appNetworkDoInactivateUnderlayNetworkWithNetworkInstance(
				ctx, status, ulStatus, ipsets)
		} else {
			appNetworkDoInactivateUnderlayNetworkWithNetworkObject(
				ctx, status, ulStatus, ipsets)
		}
	}
}

func appNetworkDoInactivateUnderlayNetworkWithNetworkInstance(
	ctx *zedrouterContext,
	status *types.AppNetworkStatus,
	ulStatus *types.UnderlayNetworkStatus,
	ipsets []string) {

	bridgeName := ulStatus.Bridge

	netconfig := lookupNetworkInstanceConfig(ctx,
		ulStatus.Network.String())
	if netconfig == nil {
		errStr := fmt.Sprintf("no network config for %s",
			ulStatus.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "lookupNetworkObjectConfig", err)
		return
	}
	netstatus := lookupNetworkInstanceStatus(ctx,
		ulStatus.Network.String())
	if netstatus == nil {
		// We had a netconfig but no status!
		errStr := fmt.Sprintf("no network status for %s",
			ulStatus.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "doInactivate underlay", err)
		return
	}
	// We ignore any errors in netstatus

	if ulStatus.Mac != "" {
		// XXX or change type of VifInfo.Mac?
		mac, err := net.ParseMAC(ulStatus.Mac)
		if err != nil {
			log.Fatal("ParseMAC failed: ",
				ulStatus.Mac, err)
		}
		err = releaseIPv4FromNetworkInstance(ctx, netstatus, mac)
		if err != nil {
			// XXX publish error?
			addError(ctx, status, "releaseIPv4", err)
		}
	}

	appIPAddr := ulStatus.AssignedIPAddr
	if appIPAddr != "" {
		removehostDnsmasq(bridgeName, ulStatus.Mac,
			appIPAddr)
	}

	// XXX Could ulStatus.Vif not be set? Means we didn't add
	if ulStatus.Vif != "" {
		err := deleteACLConfiglet(bridgeName, ulStatus.Vif, false,
			ulStatus.ACLs, ulStatus.BridgeIPAddr, appIPAddr)
		if err != nil {
			addError(ctx, status, "deleteACL", err)
		}
	} else {
		log.Warnf("doInactivate(%s): no vifName for bridge %s for %s\n",
			status.UUIDandVersion, bridgeName,
			status.DisplayName)
	}

	// Delete underlay hosts file for this app
	hostsDirpath := runDirname + "/hosts." + bridgeName
	removeFromHostsConfiglet(hostsDirpath,
		status.DisplayName)
	// Look for added or deleted ipsets
	newIpsets, staleIpsets, restartDnsmasq := diffIpsets(ipsets,
		netstatus.BridgeIPSets)

	if restartDnsmasq && ulStatus.BridgeIPAddr != "" {
		stopDnsmasq(bridgeName, true, false)
		createDnsmasqConfigletForNetworkInstance(bridgeName,
			ulStatus.BridgeIPAddr, netconfig, hostsDirpath,
			newIpsets, false)
		startDnsmasq(bridgeName)
	}
	netstatus.BridgeIPSets = newIpsets
	log.Infof("set BridgeIPSets to %v for %s", newIpsets, netstatus.Key())
	maybeRemoveStaleIpsets(staleIpsets)
}

func appNetworkDoInactivateUnderlayNetworkWithNetworkObject(
	ctx *zedrouterContext,
	status *types.AppNetworkStatus,
	ulStatus *types.UnderlayNetworkStatus,
	ipsets []string) {

	bridgeName := ulStatus.Bridge

	netconfig := lookupNetworkObjectConfig(ctx,
		ulStatus.Network.String())
	if netconfig == nil {
		errStr := fmt.Sprintf("no network config for %s",
			ulStatus.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "lookupNetworkObjectConfig", err)
		return
	}
	netstatus := lookupNetworkObjectStatus(ctx,
		ulStatus.Network.String())
	if netstatus == nil {
		// We had a netconfig but no status!
		errStr := fmt.Sprintf("no network status for %s",
			ulStatus.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "doInactivate underlay", err)
		return
	}
	// We ignore any errors in netstatus

	if ulStatus.Mac != "" {
		// XXX or change type of VifInfo.Mac?
		mac, err := net.ParseMAC(ulStatus.Mac)
		if err != nil {
			log.Fatal("ParseMAC failed: ",
				ulStatus.Mac, err)
		}
		err = releaseIPv4(ctx, netstatus, mac)
		if err != nil {
			// XXX publish error?
			addError(ctx, status, "releaseIPv4", err)
		}
	}

	appIPAddr := ulStatus.AssignedIPAddr
	if appIPAddr != "" {
		removehostDnsmasq(bridgeName, ulStatus.Mac,
			appIPAddr)
	}

	// XXX Could ulStatus.Vif not be set? Means we didn't add
	if ulStatus.Vif != "" {
		err := deleteACLConfiglet(bridgeName, ulStatus.Vif, false,
			ulStatus.ACLs, ulStatus.BridgeIPAddr, appIPAddr)
		if err != nil {
			addError(ctx, status, "deleteACL", err)
		}
	} else {
		log.Warnf("doInactivate(%s): no vifName for bridge %s for %s\n",
			status.UUIDandVersion, bridgeName,
			status.DisplayName)
	}

	// Delete underlay hosts file for this app
	hostsDirpath := runDirname + "/hosts." + bridgeName
	removeFromHostsConfiglet(hostsDirpath,
		status.DisplayName)
	// Look for added or deleted ipsets
	newIpsets, staleIpsets, restartDnsmasq := diffIpsets(ipsets,
		netstatus.BridgeIPSets)

	if restartDnsmasq && ulStatus.BridgeIPAddr != "" {
		stopDnsmasq(bridgeName, true, false)
		createDnsmasqConfiglet(bridgeName,
			ulStatus.BridgeIPAddr, netconfig, hostsDirpath,
			newIpsets, false)
		startDnsmasq(bridgeName)
	}
	netstatus.BridgeIPSets = newIpsets
	log.Infof("set BridgeIPSets to %v for %s", newIpsets, netstatus.Key())
	maybeRemoveStaleIpsets(staleIpsets)
}

func appNetworkDoInactivateAllOverlayNetworks(ctx *zedrouterContext,
	status *types.AppNetworkStatus,
	ipsets []string) {
	for olNum := 1; olNum <= len(status.OverlayNetworkList); olNum++ {
		log.Debugf("doInactivate olNum %d\n", olNum)
		// Need to check that index exists XXX remove
		if len(status.OverlayNetworkList) < olNum {
			log.Errorf("Missing status for overlay %d; can not clean up\n",
				olNum)
			continue
		}
		olStatus := &status.OverlayNetworkList[olNum-1]
		log.Infof("doInactivate olNum %d: %v\n", olNum, olStatus)
		if olStatus.UsesNetworkInstance {
			appNetworkDoInactivateOverlayNetworkWithNetworkInstance(
				ctx, status, olStatus, ipsets)
		} else {
			appNetworkDoInactivateOverlayNetworkWithNetworkObject(
				ctx, status, olStatus, ipsets)
		}

	}
	// XXX check if any IIDs are now unreferenced and delete them
	// XXX requires looking at all of configDir and statusDir
}

func appNetworkDoInactivateOverlayNetworkWithNetworkInstance(
	ctx *zedrouterContext,
	status *types.AppNetworkStatus,
	olStatus *types.OverlayNetworkStatus,
	ipsets []string) {

	bridgeName := olStatus.Bridge

	netconfig := lookupNetworkInstanceConfig(ctx,
		olStatus.Network.String())
	if netconfig == nil {
		errStr := fmt.Sprintf("no network config for %s",
			olStatus.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "lookupNetworkObjectStatus", err)
		return
	}
	netstatus := lookupNetworkInstanceStatus(ctx,
		olStatus.Network.String())
	if netstatus == nil {
		// We had a netconfig but no status!
		errStr := fmt.Sprintf("no network status for %s",
			olStatus.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "doInactivate overlay", err)
		return
	}
	// We ignore any errors in netstatus

	removehostDnsmasq(bridgeName, olStatus.Mac,
		olStatus.EID.String())

	// Delete ACLs
	// XXX Could olStatus.Vif not be set? Means we didn't add
	if olStatus.Vif != "" {
		err := deleteACLConfiglet(bridgeName, olStatus.Vif, false,
			olStatus.ACLs, olStatus.BridgeIPAddr,
			olStatus.EID.String())
		if err != nil {
			addError(ctx, status, "deleteACL", err)
		}
	} else {
		log.Warnf("doInactivate(%s): no vifName for bridge %s for %s\n",
			status.UUIDandVersion, bridgeName,
			status.DisplayName)
	}

	// Delete underlay hosts file for this app
	hostsDirpath := runDirname + "/hosts." + bridgeName
	removeFromHostsConfiglet(hostsDirpath, status.DisplayName)

	deleteDefaultIpsetConfiglet(olStatus.Vif, true)

	// Look for added or deleted ipsets
	newIpsets, staleIpsets, restartDnsmasq := diffIpsets(ipsets,
		netstatus.BridgeIPSets)

	if restartDnsmasq && olStatus.BridgeIPAddr != "" {
		stopDnsmasq(bridgeName, true, false)
		createDnsmasqConfigletForNetworkInstance(bridgeName,
			olStatus.BridgeIPAddr, netconfig, hostsDirpath,
			newIpsets, netstatus.Ipv4Eid)
		startDnsmasq(bridgeName)
	}
	netstatus.BridgeIPSets = newIpsets
	log.Infof("set BridgeIPSets to %v for %s", newIpsets, netstatus.Key())
	maybeRemoveStaleIpsets(staleIpsets)

	// Delete route towards app instance
	deleteAppInstaneOverlayRoute(ctx, status, olStatus)

	// Delete LISP configlets
	deleteLispConfiglet(lispRunDirname, false,
		netstatus.LispConfig.IID, olStatus.EID,
		olStatus.AppIPAddr, *ctx.deviceNetworkStatus,
		ctx.legacyDataPlane)
}

func deleteAppInstaneOverlayRoute(
	ctx *zedrouterContext,
	status *types.AppNetworkStatus,
	olStatus *types.OverlayNetworkStatus) {
	bridgeName := olStatus.Bridge
	oLink, err := findBridge(bridgeName)
	if err != nil {
		addError(ctx, status, "findBridge", err)
		log.Infof("deleteAppInstaneOverlayRoute done for %s\n",
			status.DisplayName)
		return
	}
	var subnetSuffix string

	EID := olStatus.AppIPAddr
	isIPv6 := (EID.To4() == nil)
	if isIPv6 {
		subnetSuffix = "/128"
	} else {
		subnetSuffix = "/32"
	}
	_, ipnet, err := net.ParseCIDR(EID.String() + subnetSuffix)
	if err != nil {
		errStr := fmt.Sprintf("ParseCIDR %s failed: %v",
			EID.String()+subnetSuffix, err)
		addError(ctx, status, "deleteAppInstaneOverlayRoute",
			errors.New(errStr))
		log.Infof("deleteAppInstaneOverlayRoute done for %s\n",
			status.DisplayName)
		return
	}
	rt := netlink.Route{Dst: ipnet, LinkIndex: oLink.Index}
	if err := netlink.RouteDel(&rt); err != nil {
		errStr := fmt.Sprintf("RouteDelete %s failed: %s",
			EID, err)
		addError(ctx, status, "deleteAppInstaneOverlayRoute",
			errors.New(errStr))
		log.Infof("deleteAppInstaneOverlayRoute done for %s\n",
			status.DisplayName)
	}
}

func appNetworkDoInactivateOverlayNetworkWithNetworkObject(
	ctx *zedrouterContext,
	status *types.AppNetworkStatus,
	olStatus *types.OverlayNetworkStatus,
	ipsets []string) {

	bridgeName := olStatus.Bridge

	netconfig := lookupNetworkObjectConfig(ctx,
		olStatus.Network.String())
	if netconfig == nil {
		errStr := fmt.Sprintf("no network config for %s",
			olStatus.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "lookupNetworkObjectStatus", err)
		return
	}
	netstatus := lookupNetworkObjectStatus(ctx,
		olStatus.Network.String())
	if netstatus == nil {
		// We had a netconfig but no status!
		errStr := fmt.Sprintf("no network status for %s",
			olStatus.Network.String())
		err := errors.New(errStr)
		addError(ctx, status, "doInactivate overlay", err)
		return
	}
	// We ignore any errors in netstatus

	removehostDnsmasq(bridgeName, olStatus.Mac,
		olStatus.EID.String())

	// Delete ACLs
	// XXX Could olStatus.Vif not be set? Means we didn't add
	if olStatus.Vif != "" {
		err := deleteACLConfiglet(bridgeName, olStatus.Vif, false,
			olStatus.ACLs, olStatus.BridgeIPAddr,
			olStatus.EID.String())
		if err != nil {
			addError(ctx, status, "deleteACL", err)
		}
	} else {
		log.Warnf("doInactivate(%s): no vifName for bridge %s for %s\n",
			status.UUIDandVersion, bridgeName,
			status.DisplayName)
	}

	// Delete underlay hosts file for this app
	hostsDirpath := runDirname + "/hosts." + bridgeName
	removeFromHostsConfiglet(hostsDirpath, status.DisplayName)

	deleteDefaultIpsetConfiglet(olStatus.Vif, true)

	// Look for added or deleted ipsets
	newIpsets, staleIpsets, restartDnsmasq := diffIpsets(ipsets,
		netstatus.BridgeIPSets)

	if restartDnsmasq && olStatus.BridgeIPAddr != "" {
		stopDnsmasq(bridgeName, true, false)
		createDnsmasqConfiglet(bridgeName,
			olStatus.BridgeIPAddr, netconfig, hostsDirpath,
			newIpsets, netstatus.Ipv4Eid)
		startDnsmasq(bridgeName)
	}
	netstatus.BridgeIPSets = newIpsets
	log.Infof("set BridgeIPSets to %v for %s", newIpsets, netstatus.Key())
	maybeRemoveStaleIpsets(staleIpsets)

	// If service does not exist overlays would not have been created
	serviceStatus := lookupAppLink(ctx, olStatus.Network)
	if serviceStatus == nil {
		// Lisp service might already have been deleted.
		// As part of Lisp service deletion, we delete all overlays.
		return
	}

	// Delete LISP configlets
	deleteLispConfiglet(lispRunDirname, false,
		serviceStatus.LispStatus.IID, olStatus.EID,
		olStatus.AppIPAddr, *ctx.deviceNetworkStatus,
		ctx.legacyDataPlane)
}

func doInactivateAppNetworkWithMgmtLisp(
	ctx *zedrouterContext,
	status *types.AppNetworkStatus) {

	if status == nil || !status.IsZedmanager {
		log.Fatalf("doInactivateAppNetworkWithMgmtLisp - Invalid State. "+
			"status: %v", status)
	}
	appNum := status.AppNum
	if len(status.OverlayNetworkList) != 1 ||
		len(status.UnderlayNetworkList) != 0 {
		errStr := "Malformed IsZedmanager status; ignored"
		addError(ctx, status, "doInactivate",
			errors.New(errStr))
		log.Infof("doInactivate done for %s\n",
			status.DisplayName)
		return
	}
	// Remove global state for device
	deviceEID = net.IP{}
	deviceIID = 0
	additionalInfoDevice = nil

	olNum := 1
	olStatus := &status.OverlayNetworkList[0]
	olIfname := "dbo" + strconv.Itoa(olNum) + "x" +
		strconv.Itoa(appNum)
	// Assume there is no UUID for management overlay

	// Delete the address from loopback
	// Delete fd00::/8 route
	// Delete fe80::1 neighbor

	//    ip addr del ${EID}/128 dev ${olIfname}
	EID := status.OverlayNetworkList[0].EID
	addr, err := netlink.ParseAddr(EID.String() + "/128")
	if err != nil {
		errStr := fmt.Sprintf("ParseAddr %s failed: %s",
			EID, err)
		addError(ctx, status, "doInactivate",
			errors.New(errStr))
		log.Infof("doInactivate done for %s\n",
			status.DisplayName)
		return
	}
	attrs := netlink.NewLinkAttrs()
	attrs.Name = olIfname
	oLink := &netlink.Dummy{LinkAttrs: attrs}
	// XXX can we skip explicit deletes and just remove the oLink?
	if err := netlink.AddrDel(oLink, addr); err != nil {
		errStr := fmt.Sprintf("AddrDel %s failed: %s",
			EID, err)
		addError(ctx, status, "doInactivate",
			errors.New(errStr))
	}

	//    ip route del fd00::/8 via fe80::1 dev $intf
	index := oLink.Attrs().Index
	_, ipnet, err := net.ParseCIDR("fd00::/8")
	if err != nil {
		log.Fatal("ParseCIDR fd00::/8 failed:\n", err)
	}
	via := net.ParseIP("fe80::1")
	if via == nil {
		log.Fatal("ParseIP fe80::1 failed: ", err)
	}
	rt := netlink.Route{Dst: ipnet, LinkIndex: index, Gw: via}
	if err := netlink.RouteDel(&rt); err != nil {
		errStr := fmt.Sprintf("RouteDel fd00::/8 failed: %s",
			err)
		addError(ctx, status, "doInactivate",
			errors.New(errStr))
	}
	//    ip nei del fe80::1 lladdr 0:0:0:0:0:1 dev $intf
	neigh := netlink.Neigh{LinkIndex: index, IP: via}
	if err := netlink.NeighDel(&neigh); err != nil {
		errStr := fmt.Sprintf("NeighDel fe80::1 failed: %s",
			err)
		addError(ctx, status, "doInactivate",
			errors.New(errStr))
	}

	// Remove link and associated addresses
	netlink.LinkDel(oLink)

	// Delete overlay hosts file
	hostsDirpath := runDirname + "/hosts." + olIfname
	deleteHostsConfiglet(hostsDirpath, true)

	// Default ipset
	deleteDefaultIpsetConfiglet(olIfname, true)

	// Delete ACLs
	err = deleteACLConfiglet(olIfname, olIfname, true, olStatus.ACLs,
		"", "")
	if err != nil {
		addError(ctx, status, "deleteACL", err)
	}

	// Delete LISP configlets
	deleteLispConfiglet(lispRunDirname, true, olStatus.MgmtIID,
		olStatus.EID, olStatus.AppIPAddr,
		*ctx.deviceNetworkStatus, ctx.legacyDataPlane)
	status.Activated = false
	publishAppNetworkStatus(ctx, status)
	log.Infof("doInactivate done for %s\n", status.DisplayName)
}

func pkillUserArgs(userName string, match string, printOnError bool) {
	cmd := "pkill"
	args := []string{
		// XXX note that alpine does not support -u
		// XXX		"-u",
		// XXX		userName,
		"-f",
		match,
	}
	var err error
	var out []byte
	for i := 0; i < 3; i++ {
		out, err = wrap.Command(cmd, args...).CombinedOutput()
		if err == nil {
			break
		}
		if printOnError {
			log.Warnf("Retrying failed command %v %v: %s output %s",
				cmd, args, err, out)
		}
		time.Sleep(time.Second)
	}
	if err != nil && printOnError {
		log.Errorf("Command %v %v failed: %s output %s\n",
			cmd, args, err, out)
	}
}

func handleGlobalConfigModify(ctxArg interface{}, key string,
	statusArg interface{}) {

	ctx := ctxArg.(*zedrouterContext)
	if key != "global" {
		log.Infof("handleGlobalConfigModify: ignoring %s\n", key)
		return
	}
	log.Infof("handleGlobalConfigModify for %s\n", key)
	debug, _ = agentlog.HandleGlobalConfig(ctx.subGlobalConfig, agentName,
		debugOverride)
	log.Infof("handleGlobalConfigModify done for %s\n", key)
}

func handleGlobalConfigDelete(ctxArg interface{}, key string,
	statusArg interface{}) {

	ctx := ctxArg.(*zedrouterContext)
	if key != "global" {
		log.Infof("handleGlobalConfigDelete: ignoring %s\n", key)
		return
	}
	log.Infof("handleGlobalConfigDelete for %s\n", key)
	debug, _ = agentlog.HandleGlobalConfig(ctx.subGlobalConfig, agentName,
		debugOverride)
	log.Infof("handleGlobalConfigDelete done for %s\n", key)
}

func handleAAModify(ctxArg interface{}, key string,
	statusArg interface{}) {

	ctx := ctxArg.(*zedrouterContext)
	status := cast.CastAssignableAdapters(statusArg)
	if key != "global" {
		log.Infof("handleAAModify: ignoring %s\n", key)
		return
	}
	log.Infof("handleAAModify() %+v\n", status)
	*ctx.assignableAdapters = status
	log.Infof("handleAAModify() done\n")
}

func handleAADelete(ctxArg interface{}, key string,
	statusArg interface{}) {

	ctx := ctxArg.(*zedrouterContext)
	if key != "global" {
		log.Infof("handleAADelete: ignoring %s\n", key)
		return
	}
	log.Infof("handleAADelete()\n")
	ctx.assignableAdapters.Initialized = false
	log.Infof("handleAADelete() done\n")
}

func handleDNSModify(ctxArg interface{}, key string, statusArg interface{}) {

	status := cast.CastDeviceNetworkStatus(statusArg)
	ctx := ctxArg.(*zedrouterContext)
	if key != "global" {
		log.Infof("handleDNSModify: ignoring %s\n", key)
		return
	}
	log.Infof("handleDNSModify for %s\n", key)
	if status.Testing {
		log.Infof("handleDNSModify ignoring Testing\n")
		return
	}
	if cmp.Equal(ctx.deviceNetworkStatus, status) {
		log.Infof("handleDNSModify no change\n")
		return
	}
	log.Infof("handleDNSModify: changed %v",
		cmp.Diff(ctx.deviceNetworkStatus, status))
	*ctx.deviceNetworkStatus = status
	maybeHandleDNS(ctx)
	log.Infof("handleDNSModify done for %s\n", key)
}

func handleDNSDelete(ctxArg interface{}, key string,
	statusArg interface{}) {

	log.Infof("handleDNSDelete for %s\n", key)
	ctx := ctxArg.(*zedrouterContext)

	if key != "global" {
		log.Infof("handleDNSDelete: ignoring %s\n", key)
		return
	}
	*ctx.deviceNetworkStatus = types.DeviceNetworkStatus{}
	maybeHandleDNS(ctx)
	log.Infof("handleDNSDelete done for %s\n", key)
}
