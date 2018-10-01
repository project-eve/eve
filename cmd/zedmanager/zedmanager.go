// Copyright (c) 2017-2018 Zededa, Inc.
// All rights reserved.

// Get AppInstanceConfig from zedagent, drive config to Downloader, Verifier,
// IdentityMgr, and Zedrouter. Collect status from those services and make
// the combined AppInstanceStatus available to zedagent.

package zedmanager

import (
	"flag"
	"fmt"
	"github.com/google/go-cmp/cmp"
	log "github.com/sirupsen/logrus"
	"github.com/zededa/go-provision/agentlog"
	"github.com/zededa/go-provision/cast"
	"github.com/zededa/go-provision/devicenetwork"
	"github.com/zededa/go-provision/pidfile"
	"github.com/zededa/go-provision/pubsub"
	"github.com/zededa/go-provision/types"
	"os"
)

const (
	appImgObj = "appImg.obj"
	certObj   = "cert.obj"
	agentName = "zedmanager"

	certificateDirname = persistDir + "/certs"
)

// Set from Makefile
var Version = "No version specified"

// State used by handlers
type zedmanagerContext struct {
	configRestarted         bool
	verifierRestarted       bool
	subAppInstanceConfig    *pubsub.Subscription
	pubAppInstanceStatus    *pubsub.Publication
	subDeviceNetworkStatus  *pubsub.Subscription
	pubAppNetworkConfig     *pubsub.Publication
	subAppNetworkStatus     *pubsub.Subscription
	pubDomainConfig         *pubsub.Publication
	subDomainStatus         *pubsub.Subscription
	pubEIDConfig            *pubsub.Publication
	subEIDStatus            *pubsub.Subscription
	subCertObjStatus        *pubsub.Subscription
	pubAppImgDownloadConfig *pubsub.Publication
	subAppImgDownloadStatus *pubsub.Subscription
	pubAppImgVerifierConfig *pubsub.Publication
	subAppImgVerifierStatus *pubsub.Subscription
	subDatastoreConfig      *pubsub.Subscription
	subGlobalConfig         *pubsub.Subscription
}

var deviceNetworkStatus types.DeviceNetworkStatus

var debug = false
var debugOverride bool // From command line arg

func Run() {
	logf, err := agentlog.Init(agentName)
	if err != nil {
		log.Fatal(err)
	}
	defer logf.Close()

	versionPtr := flag.Bool("v", false, "Version")
	debugPtr := flag.Bool("d", false, "Debug flag")
	flag.Parse()
	debug = *debugPtr
	debugOverride = debug
	if debugOverride {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	if *versionPtr {
		fmt.Printf("%s: %s\n", os.Args[0], Version)
		return
	}
	if err := pidfile.CheckAndCreatePidfile(agentName); err != nil {
		log.Fatal(err)
	}
	log.Infof("Starting %s\n", agentName)

	// Any state needed by handler functions
	ctx := zedmanagerContext{}

	// Create publish before subscribing and activating subscriptions
	pubAppInstanceStatus, err := pubsub.Publish(agentName,
		types.AppInstanceStatus{})
	if err != nil {
		log.Fatal(err)
	}
	ctx.pubAppInstanceStatus = pubAppInstanceStatus
	pubAppInstanceStatus.ClearRestarted()

	pubAppNetworkConfig, err := pubsub.Publish(agentName,
		types.AppNetworkConfig{})
	if err != nil {
		log.Fatal(err)
	}
	ctx.pubAppNetworkConfig = pubAppNetworkConfig
	pubAppNetworkConfig.ClearRestarted()

	pubDomainConfig, err := pubsub.Publish(agentName,
		types.DomainConfig{})
	if err != nil {
		log.Fatal(err)
	}
	ctx.pubDomainConfig = pubDomainConfig
	pubDomainConfig.ClearRestarted()

	pubEIDConfig, err := pubsub.Publish(agentName,
		types.EIDConfig{})
	if err != nil {
		log.Fatal(err)
	}
	ctx.pubEIDConfig = pubEIDConfig
	pubEIDConfig.ClearRestarted()

	pubAppImgDownloadConfig, err := pubsub.PublishScope(agentName,
		appImgObj, types.DownloaderConfig{})
	if err != nil {
		log.Fatal(err)
	}
	pubAppImgDownloadConfig.ClearRestarted()
	ctx.pubAppImgDownloadConfig = pubAppImgDownloadConfig

	pubAppImgVerifierConfig, err := pubsub.PublishScope(agentName,
		appImgObj, types.VerifyImageConfig{})
	if err != nil {
		log.Fatal(err)
	}
	pubAppImgVerifierConfig.ClearRestarted()
	ctx.pubAppImgVerifierConfig = pubAppImgVerifierConfig

	// Look for global config such as log levels
	subGlobalConfig, err := pubsub.Subscribe("", types.GlobalConfig{},
		false, &ctx)
	if err != nil {
		log.Fatal(err)
	}
	subGlobalConfig.ModifyHandler = handleGlobalConfigModify
	subGlobalConfig.DeleteHandler = handleGlobalConfigDelete
	ctx.subGlobalConfig = subGlobalConfig
	subGlobalConfig.Activate()

	// Get AppInstanceConfig from zedagent
	subAppInstanceConfig, err := pubsub.Subscribe("zedagent",
		types.AppInstanceConfig{}, false, &ctx)
	if err != nil {
		log.Fatal(err)
	}
	subAppInstanceConfig.ModifyHandler = handleAppInstanceConfigModify
	subAppInstanceConfig.DeleteHandler = handleAppInstanceConfigDelete
	subAppInstanceConfig.RestartHandler = handleConfigRestart
	ctx.subAppInstanceConfig = subAppInstanceConfig
	subAppInstanceConfig.Activate()

	// Look for DatastoreConfig from zedagent
	// No handlers since we look at collection when we need to
	subDatastoreConfig, err := pubsub.Subscribe("zedagent",
		types.DatastoreConfig{}, false, &ctx)
	if err != nil {
		log.Fatal(err)
	}
	subDatastoreConfig.ModifyHandler = handleDatastoreConfigModify
	subDatastoreConfig.DeleteHandler = handleDatastoreConfigDelete
	ctx.subDatastoreConfig = subDatastoreConfig
	subDatastoreConfig.Activate()

	// Get AppNetworkStatus from zedrouter
	subAppNetworkStatus, err := pubsub.Subscribe("zedrouter",
		types.AppNetworkStatus{}, false, &ctx)
	if err != nil {
		log.Fatal(err)
	}
	subAppNetworkStatus.ModifyHandler = handleAppNetworkStatusModify
	subAppNetworkStatus.DeleteHandler = handleAppNetworkStatusDelete
	subAppNetworkStatus.RestartHandler = handleZedrouterRestarted
	ctx.subAppNetworkStatus = subAppNetworkStatus
	subAppNetworkStatus.Activate()

	// Get DomainStatus from domainmgr
	subDomainStatus, err := pubsub.Subscribe("domainmgr",
		types.DomainStatus{}, false, &ctx)
	if err != nil {
		log.Fatal(err)
	}
	subDomainStatus.ModifyHandler = handleDomainStatusModify
	subDomainStatus.DeleteHandler = handleDomainStatusDelete
	ctx.subDomainStatus = subDomainStatus
	subDomainStatus.Activate()

	// Look for DownloaderStatus from downloader
	subAppImgDownloadStatus, err := pubsub.SubscribeScope("downloader",
		appImgObj, types.DownloaderStatus{}, false, &ctx)
	if err != nil {
		log.Fatal(err)
	}
	subAppImgDownloadStatus.ModifyHandler = handleDownloaderStatusModify
	subAppImgDownloadStatus.DeleteHandler = handleDownloaderStatusDelete
	ctx.subAppImgDownloadStatus = subAppImgDownloadStatus
	subAppImgDownloadStatus.Activate()

	// Look for VerifyImageStatus from verifier
	subAppImgVerifierStatus, err := pubsub.SubscribeScope("verifier",
		appImgObj, types.VerifyImageStatus{}, false, &ctx)
	if err != nil {
		log.Fatal(err)
	}
	subAppImgVerifierStatus.ModifyHandler = handleVerifyImageStatusModify
	subAppImgVerifierStatus.DeleteHandler = handleVerifyImageStatusDelete
	subAppImgVerifierStatus.RestartHandler = handleVerifierRestarted
	ctx.subAppImgVerifierStatus = subAppImgVerifierStatus
	subAppImgVerifierStatus.Activate()

	// Get IdentityStatus from identitymgr
	subEIDStatus, err := pubsub.Subscribe("identitymgr",
		types.EIDStatus{}, false, &ctx)
	if err != nil {
		log.Fatal(err)
	}
	subEIDStatus.ModifyHandler = handleEIDStatusModify
	subEIDStatus.DeleteHandler = handleEIDStatusDelete
	subEIDStatus.RestartHandler = handleIdentitymgrRestarted
	ctx.subEIDStatus = subEIDStatus
	subEIDStatus.Activate()

	subDeviceNetworkStatus, err := pubsub.Subscribe("zedrouter",
		types.DeviceNetworkStatus{}, false, &ctx)
	if err != nil {
		log.Fatal(err)
	}
	subDeviceNetworkStatus.ModifyHandler = handleDNSModify
	subDeviceNetworkStatus.DeleteHandler = handleDNSDelete
	ctx.subDeviceNetworkStatus = subDeviceNetworkStatus
	subDeviceNetworkStatus.Activate()

	// Look for CertObjStatus from zedagent
	subCertObjStatus, err := pubsub.Subscribe("zedagent",
		types.CertObjStatus{}, false, &ctx)
	if err != nil {
		log.Fatal(err)
	}
	subCertObjStatus.ModifyHandler = handleCertObjStatusModify
	subCertObjStatus.DeleteHandler = handleCertObjStatusDelete
	ctx.subCertObjStatus = subCertObjStatus
	subCertObjStatus.Activate()

	// First we process the verifierStatus to avoid downloading
	// an image we already have in place.
	log.Infof("Handling initial verifier Status\n")
	for !ctx.verifierRestarted {
		select {
		case change := <-subGlobalConfig.C:
			subGlobalConfig.ProcessChange(change)

		case change := <-subAppImgVerifierStatus.C:
			subAppImgVerifierStatus.ProcessChange(change)
			if ctx.verifierRestarted {
				log.Infof("Verifier reported restarted\n")
			}
		}
	}

	log.Infof("Handling all inputs\n")
	for {
		select {
		case change := <-subGlobalConfig.C:
			subGlobalConfig.ProcessChange(change)

		// handle cert ObjectsChanges
		case change := <-subCertObjStatus.C:
			subCertObjStatus.ProcessChange(change)

		case change := <-subAppImgDownloadStatus.C:
			subAppImgDownloadStatus.ProcessChange(change)

		case change := <-subAppImgVerifierStatus.C:
			subAppImgVerifierStatus.ProcessChange(change)

		case change := <-subEIDStatus.C:
			subEIDStatus.ProcessChange(change)

		case change := <-subAppNetworkStatus.C:
			subAppNetworkStatus.ProcessChange(change)

		case change := <-subDomainStatus.C:
			subDomainStatus.ProcessChange(change)

		case change := <-subAppInstanceConfig.C:
			subAppInstanceConfig.ProcessChange(change)

		case change := <-subDatastoreConfig.C:
			subDatastoreConfig.ProcessChange(change)

		case change := <-subDeviceNetworkStatus.C:
			subDeviceNetworkStatus.ProcessChange(change)
		}
	}
}

// After zedagent has waited for its config and set restarted for
// AppInstanceConfig (which triggers this callback) we propagate a sequence of
// restarts so that the agents don't do extra work.
// We propagate a seqence of restarted from the zedmanager config
// and verifier status to identitymgr, then from identitymgr to zedrouter,
// and finally from zedrouter to domainmgr.
// This removes the need for extra downloads/verifications and extra copying
// of the rootfs in domainmgr.
func handleConfigRestart(ctxArg interface{}, done bool) {
	ctx := ctxArg.(*zedmanagerContext)

	log.Infof("handleConfigRestart(%v)\n", done)
	if done {
		ctx.configRestarted = true
		if ctx.verifierRestarted {
			ctx.pubEIDConfig.SignalRestarted()
		}
	}
}

func handleVerifierRestarted(ctxArg interface{}, done bool) {
	ctx := ctxArg.(*zedmanagerContext)

	log.Infof("handleVerifierRestarted(%v)\n", done)
	if done {
		ctx.verifierRestarted = true
		if ctx.configRestarted {
			ctx.pubEIDConfig.SignalRestarted()
		}
	}
}

func handleIdentitymgrRestarted(ctxArg interface{}, done bool) {
	ctx := ctxArg.(*zedmanagerContext)

	log.Infof("handleIdentitymgrRestarted(%v)\n", done)
	if done {
		ctx.pubAppNetworkConfig.SignalRestarted()
	}
}

func handleZedrouterRestarted(ctxArg interface{}, done bool) {
	ctx := ctxArg.(*zedmanagerContext)

	log.Infof("handleZedrouterRestarted(%v)\n", done)
	if done {
		ctx.pubDomainConfig.SignalRestarted()
	}
}

func publishAppInstanceStatus(ctx *zedmanagerContext,
	status *types.AppInstanceStatus) {

	key := status.Key()
	log.Debugf("publishAppInstanceStatus(%s)\n", key)
	pub := ctx.pubAppInstanceStatus
	pub.Publish(key, status)
}

func unpublishAppInstanceStatus(ctx *zedmanagerContext,
	status *types.AppInstanceStatus) {

	key := status.Key()
	log.Debugf("unpublishAppInstanceStatus(%s)\n", key)
	pub := ctx.pubAppInstanceStatus
	st, _ := pub.Get(key)
	if st == nil {
		log.Errorf("unpublishAppInstanceStatus(%s) not found\n", key)
		return
	}
	pub.Unpublish(key)
}

// Determine whether it is an create or modify
func handleAppInstanceConfigModify(ctxArg interface{}, key string, configArg interface{}) {

	log.Infof("handleAppInstanceConfigModify(%s)\n", key)
	ctx := ctxArg.(*zedmanagerContext)
	config := cast.CastAppInstanceConfig(configArg)
	if config.Key() != key {
		log.Errorf("handleAppInstanceConfigModify key/UUID mismatch %s vs %s; ignored %+v\n",
			key, config.Key(), config)
		return
	}
	status := lookupAppInstanceStatus(ctx, key)
	if status == nil {
		handleCreate(ctx, key, config)
	} else {
		handleModify(ctx, key, config, status)
	}
	log.Infof("handleAppInstanceConfigModify(%s) done\n", key)
}

func handleAppInstanceConfigDelete(ctxArg interface{}, key string,
	configArg interface{}) {

	log.Infof("handleAppInstanceConfigDelete(%s)\n", key)
	ctx := ctxArg.(*zedmanagerContext)
	status := lookupAppInstanceStatus(ctx, key)
	if status == nil {
		log.Infof("handleAppInstanceConfigDelete: unknown %s\n", key)
		return
	}
	handleDelete(ctx, key, status)
	log.Infof("handleAppInstanceConfigDelete(%s) done\n", key)
}

// Callers must be careful to publish any changes to NetworkObjectStatus
func lookupAppInstanceStatus(ctx *zedmanagerContext, key string) *types.AppInstanceStatus {

	pub := ctx.pubAppInstanceStatus
	st, _ := pub.Get(key)
	if st == nil {
		log.Infof("lookupAppInstanceStatus(%s) not found\n", key)
		return nil
	}
	status := cast.CastAppInstanceStatus(st)
	if status.Key() != key {
		log.Errorf("lookupAppInstanceStatus key/UUID mismatch %s vs %s; ignored %+v\n",
			key, status.Key(), status)
		return nil
	}
	return &status
}

func lookupAppInstanceConfig(ctx *zedmanagerContext, key string) *types.AppInstanceConfig {

	sub := ctx.subAppInstanceConfig
	c, _ := sub.Get(key)
	if c == nil {
		log.Infof("lookupAppInstanceConfig(%s) not found\n", key)
		return nil
	}
	config := cast.CastAppInstanceConfig(c)
	if config.Key() != key {
		log.Errorf("lookupAppInstanceConfig key/UUID mismatch %s vs %s; ignored %+v\n",
			key, config.Key(), config)
		return nil
	}
	return &config
}

func handleCreate(ctx *zedmanagerContext, key string,
	config types.AppInstanceConfig) {

	log.Infof("handleCreate(%v) for %s\n",
		config.UUIDandVersion, config.DisplayName)

	status := types.AppInstanceStatus{
		UUIDandVersion:      config.UUIDandVersion,
		DisplayName:         config.DisplayName,
		FixedResources:      config.FixedResources,
		OverlayNetworkList:  config.OverlayNetworkList,
		UnderlayNetworkList: config.UnderlayNetworkList,
		IoAdapterList:       config.IoAdapterList,
		RestartCmd:          config.RestartCmd,
		PurgeCmd:            config.PurgeCmd,
	}

	status.StorageStatusList = make([]types.StorageStatus,
		len(config.StorageConfigList))
	for i, sc := range config.StorageConfigList {
		ss := &status.StorageStatusList[i]
		ss.Name = sc.Name
		ss.ImageSha256 = sc.ImageSha256
		ss.ReadOnly = sc.ReadOnly
		ss.Preserve = sc.Preserve
		ss.Format = sc.Format
		ss.Maxsizebytes = sc.Maxsizebytes
		ss.Devtype = sc.Devtype
		ss.Target = sc.Target
	}
	status.EIDList = make([]types.EIDStatusDetails,
		len(config.OverlayNetworkList))

	publishAppInstanceStatus(ctx, &status)

	uuidStr := status.Key()
	changed := doUpdate(ctx, uuidStr, config, &status)
	if changed {
		log.Infof("handleCreate status change for %s\n",
			uuidStr)
		publishAppInstanceStatus(ctx, &status)
	}
	log.Infof("handleCreate done for %s\n", config.DisplayName)
}

func handleModify(ctx *zedmanagerContext, key string,
	config types.AppInstanceConfig, status *types.AppInstanceStatus) {
	log.Infof("handleModify(%v) for %s\n",
		config.UUIDandVersion, config.DisplayName)

	// XXX handle at least ACL and activate changes. What else?
	// Not checking the version here; assume the microservices can handle
	// some updates.

	// We detect significant changes which require a reboot and/or
	// purge of disk changes
	needPurge, needRestart := quantifyChanges(config, *status)

	if needRestart ||
		config.RestartCmd.Counter != status.RestartCmd.Counter {

		log.Infof("handleModify(%v) for %s restartcmd from %d to %d need %v\n",
			config.UUIDandVersion, config.DisplayName,
			status.RestartCmd.Counter, config.RestartCmd.Counter,
			needRestart)
		if status.Activated {
			// Will restart even if we crash/power cycle since that
			// would also restart the app. Hence we can update
			// the status counter here.
			status.RestartCmd.Counter = config.RestartCmd.Counter
			status.RestartInprogress = types.BRING_DOWN
			status.State = types.RESTARTING
		} else {
			log.Infof("handleModify(%v) for %s restartcmd ignored !Activated\n",
				config.UUIDandVersion, config.DisplayName)
			status.RestartCmd.Counter = config.RestartCmd.Counter
		}
	}
	if needPurge || config.PurgeCmd.Counter != status.PurgeCmd.Counter {
		log.Infof("handleModify(%v) for %s purgecmd from %d to %d need %v\n",
			config.UUIDandVersion, config.DisplayName,
			status.PurgeCmd.Counter, config.PurgeCmd.Counter,
			needPurge)
		// XXX need to remember purge across power cycle
		// But when we get the config after a device reboot
		// we don't have history. Add per App UUID file in /config or
		// /persist with PurgeInprogress value?
		status.PurgeCmd.Counter = config.PurgeCmd.Counter
		status.PurgeInprogress = types.DOWNLOAD
		status.State = types.PURGING
	}
	status.UUIDandVersion = config.UUIDandVersion
	publishAppInstanceStatus(ctx, status)

	uuidStr := status.Key()
	changed := doUpdate(ctx, uuidStr, config, status)
	if changed {
		log.Infof("handleModify status change for %s\n",
			uuidStr)
		publishAppInstanceStatus(ctx, status)
	}
	status.FixedResources = config.FixedResources
	status.OverlayNetworkList = config.OverlayNetworkList
	status.UnderlayNetworkList = config.UnderlayNetworkList
	status.IoAdapterList = config.IoAdapterList
	log.Infof("handleModify done for %s\n", config.DisplayName)
}

func handleDelete(ctx *zedmanagerContext, key string,
	status *types.AppInstanceStatus) {

	log.Infof("handleDelete(%v) for %s\n",
		status.UUIDandVersion, status.DisplayName)

	removeAIStatus(ctx, status)
	log.Infof("handleDelete done for %s\n", status.DisplayName)
}

// Returns needRestart, needPurge
// XXX return an enum instead?
// If there is a change to the disks, adapters, or network interfaces
// it returns needPurge.
// If there is a change to the CPU etc resources it returns needRestart
// Changes to ACLs don't result in either being returned.
func quantifyChanges(config types.AppInstanceConfig,
	status types.AppInstanceStatus) (bool, bool) {

	needPurge := false
	needRestart := false
	log.Infof("quantifyChanges for %s %s\n",
		config.Key(), config.DisplayName)
	if len(status.StorageStatusList) != len(config.StorageConfigList) {
		log.Infof("quantifyChanges len storage changed from %d to %d\n",
			len(status.StorageStatusList),
			len(config.StorageConfigList))
		needPurge = true
	} else {
		for i, sc := range config.StorageConfigList {
			ss := status.StorageStatusList[i]
			if ss.ImageSha256 != sc.ImageSha256 {
				log.Infof("quantifyChanges storage sha changed from %s to %s\n",
					ss.ImageSha256, sc.ImageSha256)
				needPurge = true
			}
			if ss.ReadOnly != sc.ReadOnly {
				log.Infof("quantifyChanges storage ReadOnly changed from %v to %v\n",
					ss.ReadOnly, sc.ReadOnly)
				needPurge = true
			}
			if ss.Preserve != sc.Preserve {
				log.Infof("quantifyChanges storage Preserve changed from %v to %v\n",
					ss.Preserve, sc.Preserve)
				needPurge = true
			}
			if ss.Format != sc.Format {
				log.Infof("quantifyChanges storage Format changed from %v to %v\n",
					ss.Format, sc.Format)
				needPurge = true
			}
			if ss.Maxsizebytes != sc.Maxsizebytes {
				log.Infof("quantifyChanges storage Maxsizebytes changed from %v to %v\n",
					ss.Maxsizebytes, sc.Maxsizebytes)
				needPurge = true
			}
			if ss.Devtype != sc.Devtype {
				log.Infof("quantifyChanges storage Devtype changed from %v to %v\n",
					ss.Devtype, sc.Devtype)
				needPurge = true
			}
		}
	}
	// Compare networks without comparing ACLs
	if len(status.OverlayNetworkList) != len(config.OverlayNetworkList) {
		log.Infof("quantifyChanges len storage changed from %d to %d\n",
			len(status.OverlayNetworkList),
			len(config.OverlayNetworkList))
		needPurge = true
	} else {
		for i, oc := range config.OverlayNetworkList {
			os := status.OverlayNetworkList[i]
			if !cmp.Equal(oc.EIDConfigDetails, os.EIDConfigDetails) {
				log.Infof("quantifyChanges EIDConfigDetails changed: %v\n",
					cmp.Diff(oc.EIDConfigDetails, os.EIDConfigDetails))
				needPurge = true
			}
			if os.AppMacAddr.String() != oc.AppMacAddr.String() {
				log.Infof("quantifyChanges AppMacAddr changed from %v to %v\n",
					os.AppMacAddr, oc.AppMacAddr)
				needPurge = true
			}
			if !os.AppIPAddr.Equal(oc.AppIPAddr) {
				log.Infof("quantifyChanges AppIPAddr changed from %v to %v\n",
					os.AppIPAddr, oc.AppIPAddr)
				needPurge = true
			}
			if os.Network != oc.Network {
				log.Infof("quantifyChanges Network changed from %v to %v\n",
					os.Network, oc.Network)
				needPurge = true
			}
			if !cmp.Equal(oc.ACLs, os.ACLs) {
				log.Infof("quantifyChanges FYI ACLs changed: %v\n",
					cmp.Diff(oc.ACLs, os.ACLs))
			}
		}
	}
	if len(status.UnderlayNetworkList) != len(config.UnderlayNetworkList) {
		log.Infof("quantifyChanges len storage changed from %d to %d\n",
			len(status.UnderlayNetworkList),
			len(config.UnderlayNetworkList))
		needPurge = true
	} else {
		for i, uc := range config.UnderlayNetworkList {
			us := status.UnderlayNetworkList[i]
			if us.AppMacAddr.String() != uc.AppMacAddr.String() {
				log.Infof("quantifyChanges AppMacAddr changed from %v to %v\n",
					us.AppMacAddr, uc.AppMacAddr)
				needPurge = true
			}
			if !us.AppIPAddr.Equal(uc.AppIPAddr) {
				log.Infof("quantifyChanges AppIPAddr changed from %v to %v\n",
					us.AppIPAddr, uc.AppIPAddr)
				needPurge = true
			}
			if us.Network != uc.Network {
				log.Infof("quantifyChanges Network changed from %v to %v\n",
					us.Network, uc.Network)
				needPurge = true
			}
			if !cmp.Equal(uc.ACLs, us.ACLs) {
				log.Infof("quantifyChanges FYI ACLs changed: %v\n",
					cmp.Diff(uc.ACLs, us.ACLs))
			}
		}
	}
	if !cmp.Equal(config.IoAdapterList, status.IoAdapterList) {
		log.Infof("quantifyChanges IoAdapterList changed: %v\n",
			cmp.Diff(config.IoAdapterList, status.IoAdapterList))
		needPurge = true
	}
	if !cmp.Equal(config.FixedResources, status.FixedResources) {
		log.Infof("quantifyChanges FixedResources changed: %v\n",
			cmp.Diff(config.FixedResources, status.FixedResources))
		needRestart = true
	}
	log.Infof("quantifyChanges for %s %s returns %v, %v\n",
		config.Key(), config.DisplayName, needPurge, needRestart)
	return needPurge, needRestart
}

func handleDNSModify(ctxArg interface{}, key string, statusArg interface{}) {

	status := cast.CastDeviceNetworkStatus(statusArg)
	if key != "global" {
		log.Debugf("handleDNSModify: ignoring %s\n", key)
		return
	}
	log.Infof("handleDNSModify for %s\n", key)
	deviceNetworkStatus = status
	devicenetwork.ProxyToEnv(deviceNetworkStatus.ProxyConfig)
	log.Infof("handleDNSModify done for %s\n", key)
}

func handleDNSDelete(ctxArg interface{}, key string, statusArg interface{}) {

	log.Infof("handleDNSDelete for %s\n", key)
	if key != "global" {
		log.Infof("handleDNSDelete: ignoring %s\n", key)
		return
	}
	deviceNetworkStatus = types.DeviceNetworkStatus{}
	devicenetwork.ProxyToEnv(deviceNetworkStatus.ProxyConfig)
	log.Infof("handleDNSDelete done for %s\n", key)
}

func handleDatastoreConfigModify(ctxArg interface{}, key string,
	configArg interface{}) {

	// XXX empty since we look at collection when we need it
	log.Infof("handleDatastoreConfigModify for %s\n", key)
}

func handleDatastoreConfigDelete(ctxArg interface{}, key string,
	configArg interface{}) {

	// XXX empty since we look at collection when we need it
	log.Infof("handleDatastoreConfigDelete for %s\n", key)
}

func handleGlobalConfigModify(ctxArg interface{}, key string,
	statusArg interface{}) {

	ctx := ctxArg.(*zedmanagerContext)
	if key != "global" {
		log.Infof("handleGlobalConfigModify: ignoring %s\n", key)
		return
	}
	log.Infof("handleGlobalConfigModify for %s\n", key)
	debug = agentlog.HandleGlobalConfig(ctx.subGlobalConfig, agentName,
		debugOverride)
	log.Infof("handleGlobalConfigModify done for %s\n", key)
}

func handleGlobalConfigDelete(ctxArg interface{}, key string,
	statusArg interface{}) {

	ctx := ctxArg.(*zedmanagerContext)
	if key != "global" {
		log.Infof("handleGlobalConfigDelete: ignoring %s\n", key)
		return
	}
	log.Infof("handleGlobalConfigDelete for %s\n", key)
	debug = agentlog.HandleGlobalConfig(ctx.subGlobalConfig, agentName,
		debugOverride)
	log.Infof("handleGlobalConfigDelete done for %s\n", key)
}
