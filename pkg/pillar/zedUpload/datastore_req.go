// Copyright(c) 2017-2018 Zededa, Inc.
// All rights reserved.

package zedUpload

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sync"
	"time"
)

var (
	SyncUnsupport = errors.New("Unsupported function")
	SyncerRetry   = errors.New("Retry - syncer full")
)

type DronaRequest struct {
	sync.RWMutex

	// Optional, you can direct request for perticular server
	syncEp    DronaEndPoint
	operation SyncOpType

	// Object that needs to be downloaded
	name      string
	localName string

	// Location to where to download
	objloc string

	// need size acknowledgement
	ackback bool

	// request is still in progress, this is just update
	inprogress bool

	// if size exceed this don't download
	sizelimit int64

	// Filled by Drona, actual size
	asize      int64
	objectSize int64

	// Filled by Drona, images list
	imgList []string

	// Filled by Drona, uploaded blob content length
	contentLength int64

	// Filled by Drona, uploaded blob MD5sum
	remoteFileMD5 string

	// Status of Download, we convert here to string because this
	// field is going to be json marshalled
	status string

	// result to be sent out
	result chan *DronaRequest

	startTime, endTime time.Time

	// Download counter
	retry int
}

// Return object local name
func (req *DronaRequest) GetLocalName() string {
	req.Lock()
	defer req.Unlock()
	if req.localName != "" {
		return req.localName
	}
	return req.name
}

// Return object error status string
func (req *DronaRequest) GetDnStatus() error {
	req.Lock()
	defer req.Unlock()
	return fmt.Errorf("Syncer Download Status on %s - error %s", req.name, req.status)
}

func (req *DronaRequest) GetUpStatus() (string, error) {
	req.Lock()
	defer req.Unlock()
	return req.objloc, fmt.Errorf("Syncer Upload Status on %s - error %s", req.name, req.status)
}

// Return is it update
func (req *DronaRequest) IsDnUpdate() bool {
	req.Lock()
	defer req.Unlock()
	return req.inprogress
}

func (req *DronaRequest) setInprogress() {
	req.Lock()
	defer req.Unlock()
	req.inprogress = true
}

func (req *DronaRequest) setStatus(status error) {
	req.Lock()
	defer req.Unlock()
	req.status = fmt.Sprintf("%v", status)
}

func (req *DronaRequest) clearInprogress() {
	req.Lock()
	defer req.Unlock()
	req.inprogress = false
}

// Return object actual synced down size
func (req *DronaRequest) GetAsize() int64 {
	req.Lock()
	defer req.Unlock()
	return req.asize
}

// Return object actual synced down size
func (req *DronaRequest) GetOsize() int64 {
	req.Lock()
	defer req.Unlock()
	return req.objectSize
}

func (req *DronaRequest) GetImageList() []string {
	req.Lock()
	defer req.Unlock()
	return req.imgList
}

func (req *DronaRequest) GetContentLength() int64 {
	req.Lock()
	defer req.Unlock()
	return req.contentLength
}

func (req *DronaRequest) GetRemoteFileMD5() string {
	req.Lock()
	defer req.Unlock()
	return req.remoteFileMD5
}

// Update the actual size
func (req *DronaRequest) updateAsize(size int64) {
	req.Lock()
	defer req.Unlock()
	req.asize = size
}

// Update the object size
func (req *DronaRequest) updateOsize(size int64) {
	req.Lock()
	defer req.Unlock()
	req.objectSize = size
}

// Return the if the object was downloaded with error
func (req *DronaRequest) IsError() bool {
	req.Lock()
	defer req.Unlock()
	if req.status == "" {
		return false
	}

	return true
}

// Return the status
func (req *DronaRequest) GetStatus() string {
	req.Lock()
	defer req.Unlock()
	return req.status
}

type SyncMetaFile struct {
	Operation SyncOpType

	// Object that needs to be downloaded
	Name string

	// Filled by Syncer, actual size
	Asize int64

	// Status of Download, we convert here to string because this
	// field is going to be json marshalled
	Status string

	EndTime   string
	StartTime string
}

// helper function to write the metafile for the transcation that is
// defined by this request
func (req *DronaRequest) WriteMetaFile(metaloc string) error {
	var internalerr error
	if metaloc != "" {
		f, err := os.OpenFile(metaloc, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			log.Printf("file create failed %s - %v", metaloc, err)
			internalerr = err
		} else {
			defer f.Close()

			wreq := &SyncMetaFile{
				Operation: req.operation,
				Name:      req.name,
				Asize:     req.asize,
				Status:    req.status,
				EndTime:   req.endTime.Format(time.RFC3339),
				StartTime: req.startTime.Format(time.RFC3339)}

			b, err := json.MarshalIndent(wreq, "", "    ")
			if err != nil {
				internalerr = err
				log.Printf("metadata %s write failed %v", req.name, err)
				b = []byte(fmt.Sprintf("Error on marshaling data into bytes %v - %v\n", req, err))
			}
			if _, err := f.Write(b); err != nil {
				internalerr = err
				log.Printf("error writing file %s, %v", metaloc, err)
			}
		}
	} else {
		internalerr = fmt.Errorf("No metafile for req %s", req.name)
	}

	return internalerr
}

func ReadMetaFile(metaloc string) (error, *DronaRequest) {
	if metaloc == "" {
		return fmt.Errorf("file path empty"), nil
	}
	sbody, serr := ioutil.ReadFile(metaloc)
	if serr != nil {
		return serr, nil
	}

	rreq := &SyncMetaFile{}
	if err := json.Unmarshal(sbody, rreq); err != nil {
		return err, nil
	}

	dnResp := DronaRequest{operation: rreq.Operation,
		name:   rreq.Name,
		asize:  rreq.Asize,
		status: rreq.Status}

	return nil, &dnResp
}

func (req *DronaRequest) Post() error {
	return req.postOnChannel()
}

func (req *DronaRequest) postOnChannel() error {
	ctx := req.syncEp.getContext()

	select {
	case ctx.reqChan <- req:
		req.startTime = time.Now()
		return nil
	default:
		return SyncerRetry
	}

	return nil
}

func (req *DronaRequest) post() {
	req.postOnChannel()
}

func (req *DronaRequest) Cancel() error {
	return nil
}
