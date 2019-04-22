// Copyright(c) 2017-2018 Zededa, Inc.
// All rights reserved.

package awsutil

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"log"
	"net/http"
	"time"
)

const (
	S3_PART_SIZE        = 5 * 1024 * 1024
	S3_PART_LEAVE_ERROR = true
)

type S3ctx struct {
	p   S3CredProvider
	ss3 *s3.S3
	dn  *s3manager.Downloader
	up  *s3manager.Uploader
}

type S3CredProvider struct {
	id, secret, token string
}

func (p *S3CredProvider) Retrieve() (credentials.Value, error) {
	token := ""
	return credentials.Value{AccessKeyID: p.id, SecretAccessKey: p.secret,
		SessionToken: token}, nil
}

func (p *S3CredProvider) IsExpired() bool {
	return true
}

func NewAwsCtx(id, secret, region string, hctx *http.Client) *S3ctx {
	ctx := S3ctx{p: S3CredProvider{id: id, secret: secret}}
	cred := credentials.NewCredentials(&ctx.p)

	cfg := aws.NewConfig()
	cfg.WithCredentials(cred)

	// regions
	cfg.WithRegion(region)

	if hctx != nil {
		cfg.WithHTTPClient(hctx)
	}

	// FIXME: We need figoure out how to do this with SSL verification
	cfg.WithDisableSSL(true)
	ctx.ss3 = s3.New(session.New(), cfg)
	ctx.up = s3manager.NewUploaderWithClient(ctx.ss3, func(u *s3manager.Uploader) {
		u.PartSize = S3_PART_SIZE
		u.LeavePartsOnError = S3_PART_LEAVE_ERROR
	})
	ctx.dn = s3manager.NewDownloaderWithClient(ctx.ss3, func(d *s3manager.Downloader) {
		d.PartSize = S3_PART_SIZE
	})

	return &ctx
}

func (s *S3ctx) CreateBucket(bname string) error {
	_, err := s.ss3.CreateBucket(&s3.CreateBucketInput{Bucket: &bname})
	if err != nil {
		log.Printf("Failed to create bucket %s/%v", bname, err)
		return err
	}

	return nil
}

func (s *S3ctx) IsBucketAvailable(bname string) (error, bool) {
	result, err := s.ss3.ListBuckets(&s3.ListBucketsInput{})
	if err != nil {
		log.Printf("Failed to list buckets %s/%s", bname, err.Error())
		return err, false
	}

	for _, bucket := range result.Buckets {
		if *bucket.Name == bname {
			return nil, true
		}
	}

	return nil, false
}

func (s *S3ctx) WaitUntilBucketExists(bname string) bool {

	// Checks for the http status and retries if 404 in 5s (returns false after 20 retries)
	// Could be used as option for IsBucketAvailable - requires listBucket access

	if err := s.ss3.WaitUntilBucketExists(&s3.HeadBucketInput{Bucket: &bname}); err != nil {
		log.Printf("Failed to wait for bucket to exist %s, %s\n", bname, err)
		return false
	}

	return true
}

func (s *S3ctx) DeleteBucket(bname string) error {
	return nil
}

func (s *S3ctx) GetObjectURL(bname, bkey string) (error, string) {
	req, _ := s.ss3.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(bname),
		Key:    aws.String(bkey)})

	// Presign a request with 1 minute expiration.
	surl, err := req.Presign(1 * time.Minute)

	return err, surl
}

func (s *S3ctx) DeleteObject(bname, bkey string) error {
	_, err := s.ss3.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(bname),
		Key:    aws.String(bkey)})
	return err
}

func (s *S3ctx) GetObjectSize(bname, bkey string) (error, int64) {
	resp, err := s.ss3.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(bname),
		Key:    aws.String(bkey)})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			return aerr, 0
		}
		return err, 0
	}

	if resp != nil {
		return nil, *resp.ContentLength
	}

	return nil, 0
}

func (s *S3ctx) GetObjectMD5(bname, bkey string) (error, string) {
	resp, err := s.ss3.HeadObject(&s3.HeadObjectInput{
		Bucket: aws.String(bname),
		Key:    aws.String(bkey)})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			return aerr, ""
		}
		return err, ""
	}
	if resp != nil {
		return nil, *resp.ETag
	}
	return nil, ""
}
