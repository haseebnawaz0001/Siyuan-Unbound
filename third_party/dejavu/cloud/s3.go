// DejaVu - Data snapshot and sync.
// Copyright (c) 2022-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package cloud

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/88250/gulu"
	"github.com/aws/aws-sdk-go-v2/aws"
	asSigner "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	as3 "github.com/aws/aws-sdk-go-v2/service/s3"
	as3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/panjf2000/ants/v2"
	"github.com/siyuan-note/dejavu/entity"
	"github.com/siyuan-note/logging"
)

// S3 描述了 S3 协议兼容的对象存储服务实现。
type S3 struct {
	*BaseCloud
	HTTPClient *http.Client
	service    *as3.Client // 用于缓存 S3 客户端
	mux        sync.Mutex  // 用于保护 service 字段的并发访问
}

// defaultCloudDir is the cloud sync directory name that keeps the historical bucket-root object layout.
const defaultCloudDir = "main"

// repoKeyPrefix returns the object key prefix for the configured cloud sync directory.
//
// Historically the S3 provider ignored Conf.Dir and wrote every object to the bucket root, so one bucket could hold
// exactly one workspace. Naming the directory now namespaces it under siyuan/<dir>/, which lets a single bucket hold
// several workspaces the way the WebDAV and local providers already do. The default directory keeps the bucket-root
// layout so existing buckets keep working without migration.
func (s3 *S3) repoKeyPrefix() string {
	if dir := s3.Conf.Dir; "" != dir && defaultCloudDir != dir {
		return path.Join("siyuan", dir, "repo")
	}
	return "repo"
}

// repoKey joins elem onto the repo key prefix for the configured cloud sync directory.
func (s3 *S3) repoKey(elem ...string) string {
	return path.Join(append([]string{s3.repoKeyPrefix()}, elem...)...)
}

func NewS3(baseCloud *BaseCloud, httpClient *http.Client) *S3 {
	return &S3{BaseCloud: baseCloud, HTTPClient: httpClient}
}

func (s3 *S3) GetRepos() (repos []*Repo, size int64, err error) {
	repos, err = s3.listRepos()
	if nil != err {
		return
	}

	for _, repo := range repos {
		size += repo.Size
	}
	return
}

func (s3 *S3) UploadObject(filePath string, overwrite bool) (length int64, err error) {
	svc := s3.getService()
	ctx, cancelFn := context.WithTimeout(context.Background(), time.Duration(s3.S3.Timeout)*time.Second)
	defer cancelFn()

	absFilePath := filepath.Join(s3.Conf.RepoPath, filePath)
	info, err := os.Stat(absFilePath)
	if nil != err {
		logging.LogErrorf("stat failed: %s", err)
		return
	}
	length = info.Size()

	file, err := os.Open(absFilePath)
	if nil != err {
		return
	}
	defer file.Close()
	key := s3.repoKey(filePath)
	_, err = svc.PutObject(ctx, &as3.PutObjectInput{
		Bucket:       aws.String(s3.Conf.S3.Bucket),
		Key:          aws.String(key),
		CacheControl: aws.String("no-cache"),
		Body:         file,
	})
	if nil != err {
		return
	}

	//logging.LogInfof("uploaded object [%s]", key)
	return
}

func (s3 *S3) UploadBytes(filePath string, data []byte, overwrite bool) (length int64, err error) {
	length = int64(len(data))
	svc := s3.getService()
	ctx, cancelFn := context.WithTimeout(context.Background(), time.Duration(s3.S3.Timeout)*time.Second)
	defer cancelFn()

	key := s3.repoKey(filePath)
	_, err = svc.PutObject(ctx, &as3.PutObjectInput{
		Bucket:       aws.String(s3.Conf.S3.Bucket),
		Key:          aws.String(key),
		CacheControl: aws.String("no-cache"),
		Body:         bytes.NewReader(data),
	})
	if nil != err {
		return
	}

	//logging.LogInfof("uploaded object [%s]", key)
	return
}

func (s3 *S3) DownloadObject(filePath string) (data []byte, err error) {
	svc := s3.getService()
	ctx, cancelFn := context.WithTimeout(context.Background(), time.Duration(s3.S3.Timeout)*time.Second)
	defer cancelFn()
	key := s3.repoKey(filePath)
	input := &as3.GetObjectInput{
		Bucket:               aws.String(s3.Conf.S3.Bucket),
		Key:                  aws.String(key),
		ResponseCacheControl: aws.String("no-cache"),
	}
	resp, err := svc.GetObject(ctx, input)
	if nil != err {
		if s3.isErrNotFound(err) {
			err = ErrCloudObjectNotFound
		}
		return
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(resp.Body)
	if nil != err {
		return
	}

	//logging.LogInfof("downloaded object [%s]", key)
	return
}

func (s3 *S3) RemoveObject(key string) (err error) {
	key = s3.repoKey(key)
	svc := s3.getService()
	ctx, cancelFn := context.WithTimeout(context.Background(), time.Duration(s3.S3.Timeout)*time.Second)
	defer cancelFn()
	_, err = svc.DeleteObject(ctx, &as3.DeleteObjectInput{
		Bucket: aws.String(s3.Conf.S3.Bucket),
		Key:    aws.String(key),
	})
	if nil != err {
		return
	}

	//logging.LogInfof("removed object [%s]", key)
	return
}

func (s3 *S3) GetTags() (tags []*Ref, err error) {
	tags, err = s3.listRepoRefs("tags")
	if nil != err {
		logging.LogErrorf("list repo tags failed: %s", err)
		return
	}
	if 1 > len(tags) {
		tags = []*Ref{}
	}
	return
}

const pageSize = 32

func (s3 *S3) GetIndexes(page int) (ret []*entity.Index, pageCount, totalCount int, err error) {
	ret = []*entity.Index{}
	data, err := s3.DownloadObject("indexes-v2.json")
	if nil != err {
		if s3.isErrNotFound(err) {
			err = nil
		}
		return
	}

	data, err = compressDecoder.DecodeAll(data, nil)
	if nil != err {
		return
	}

	indexesJSON := &Indexes{}
	if err = gulu.JSON.UnmarshalJSON(data, indexesJSON); nil != err {
		return
	}

	totalCount = len(indexesJSON.Indexes)
	pageCount = int(math.Ceil(float64(totalCount) / float64(pageSize)))

	start := (page - 1) * pageSize
	end := page * pageSize
	if end > totalCount {
		end = totalCount
	}

	for i := start; i < end; i++ {
		index, getErr := s3.repoIndex(indexesJSON.Indexes[i].ID)
		if nil != getErr {
			logging.LogWarnf("get index [%s] failed: %s", indexesJSON.Indexes[i], getErr)
			continue
		}
		if nil == index {
			continue
		}

		index.Files = nil // Optimize the performance of obtaining cloud snapshots https://github.com/siyuan-note/siyuan/issues/8387
		ret = append(ret, index)
	}
	return
}

func (s3 *S3) GetRefsFiles() (fileIDs []string, refs []*Ref, err error) {
	refs, err = s3.listRepoRefs("")
	if nil != err {
		logging.LogErrorf("list repo refs failed: %s", err)
		return
	}

	var files []string
	for _, ref := range refs {
		index, getErr := s3.repoIndex(ref.ID)
		if nil != getErr {
			err = getErr
			return
		}
		if nil == index {
			continue
		}

		files = append(files, index.Files...)
	}
	fileIDs = gulu.Str.RemoveDuplicatedElem(files)
	if 1 > len(fileIDs) {
		fileIDs = []string{}
	}
	return
}

func (s3 *S3) GetChunks(checkChunkIDs []string) (chunkIDs []string, err error) {
	var keys []string
	repoObjects := s3.repoKey("objects")
	for _, chunk := range checkChunkIDs {
		key := path.Join(repoObjects, chunk[:2], chunk[2:])
		keys = append(keys, key)
	}

	notFound, err := s3.getNotFound(keys)
	if nil != err {
		return
	}

	var notFoundChunkIDs []string
	for _, key := range notFound {
		chunkID := strings.TrimPrefix(key, repoObjects)
		chunkID = strings.ReplaceAll(chunkID, "/", "")
		notFoundChunkIDs = append(notFoundChunkIDs, chunkID)
	}

	chunkIDs = append(chunkIDs, notFoundChunkIDs...)
	chunkIDs = gulu.Str.RemoveDuplicatedElem(chunkIDs)
	if 1 > len(chunkIDs) {
		chunkIDs = []string{}
	}
	return
}

func (s3 *S3) GetIndex(id string) (index *entity.Index, err error) {
	index, err = s3.repoIndex(id)
	if nil != err {
		logging.LogErrorf("get index [%s] failed: %s", id, err)
		return
	}
	if nil == index {
		err = ErrCloudObjectNotFound
		return
	}
	return
}

func (s3 *S3) GetConcurrentReqs() (ret int) {
	ret = s3.S3.ConcurrentReqs
	if 1 > ret {
		ret = 8
	}
	if 16 < ret {
		ret = 16
	}
	return
}

func (s3 *S3) ListObjects(pathPrefix string) (ret map[string]*entity.ObjectInfo, err error) {
	ret = map[string]*entity.ObjectInfo{}
	svc := s3.getService()

	endWithSlash := strings.HasSuffix(pathPrefix, "/")
	pathPrefix = s3.repoKey(pathPrefix)
	if endWithSlash {
		pathPrefix += "/"
	}
	limit := int32(1000)
	ctx, cancelFn := context.WithTimeout(context.Background(), time.Duration(s3.S3.Timeout)*time.Second)
	defer cancelFn()

	paginator := as3.NewListObjectsV2Paginator(svc, &as3.ListObjectsV2Input{
		Bucket:  &s3.Conf.S3.Bucket,
		Prefix:  &pathPrefix,
		MaxKeys: &limit,
	})

	for paginator.HasMorePages() {
		output, pErr := paginator.NextPage(ctx)
		if nil != pErr {
			logging.LogErrorf("list objects failed: %s", pErr)
			return nil, pErr
		}

		for _, entry := range output.Contents {
			filePath := strings.TrimPrefix(*entry.Key, pathPrefix)
			if "" == filePath {
				logging.LogWarnf("skip empty file path for key [%s]", *entry.Key)
				continue
			}

			ret[filePath] = &entity.ObjectInfo{
				Path: filePath,
				Size: *entry.Size,
			}
		}
	}
	return
}

func (s3 *S3) repoIndex(id string) (ret *entity.Index, err error) {
	indexPath := s3.repoKey("indexes", id)
	info, err := s3.statFile(indexPath)
	if nil != err {
		if s3.isErrNotFound(err) {
			err = nil
		}
		return
	}
	if 1 > info.Size {
		return
	}

	data, err := s3.DownloadObject(path.Join("indexes", id))
	if nil != err {
		logging.LogErrorf("download index [%s] failed: %s", id, err)
		return
	}
	data, err = compressDecoder.DecodeAll(data, nil)
	if nil != err {
		logging.LogErrorf("decompress index [%s] failed: %s", id, err)
		return
	}
	ret = &entity.Index{}
	err = gulu.JSON.UnmarshalJSON(data, ret)
	return
}

func (s3 *S3) listRepoRefs(refPrefix string) (ret []*Ref, err error) {
	svc := s3.getService()
	ctx, cancelFn := context.WithTimeout(context.Background(), time.Duration(s3.S3.Timeout)*time.Second)
	defer cancelFn()

	prefix := s3.repoKey("refs", refPrefix)
	limit := int32(32)
	marker := ""
	for {
		output, listErr := svc.ListObjects(ctx, &as3.ListObjectsInput{
			Bucket:  &s3.Conf.S3.Bucket,
			Prefix:  &prefix,
			Marker:  &marker,
			MaxKeys: &limit,
		})
		if nil != listErr {
			return
		}

		if nil == output {
			logging.LogWarnf("list objects output is nil")
			return
		}

		marker = *output.Marker

		for _, entry := range output.Contents {
			filePath := strings.TrimPrefix(*entry.Key, s3.repoKeyPrefix()+"/")
			data, getErr := s3.DownloadObject(filePath)
			if nil != getErr {
				err = getErr
				return
			}

			id := string(data)
			info, statErr := s3.statFile(s3.repoKey("indexes", id))
			if nil != statErr {
				err = statErr
				return
			}
			if 1 > info.Size {
				continue
			}

			ret = append(ret, &Ref{
				Name:    path.Base(*entry.Key),
				ID:      id,
				Updated: entry.LastModified.Format("2006-01-02 15:04:05"),
			})
		}

		if !(*output.IsTruncated) {
			break
		}
	}
	return
}

// listRepos lists the cloud sync directories held in the configured bucket.
//
// A directory is a key prefix, not a bucket, so this enumerates the common prefixes under siyuan/ and additionally
// reports the default directory when the historical bucket-root layout is present. Listing prefixes rather than calling
// ListBuckets also means the credentials only need access to the one bucket they were configured with, which is what
// scoped keys on R2 and B2 typically grant.
func (s3 *S3) listRepos() (ret []*Repo, err error) {
	svc := s3.getService()
	ctx, cancelFn := context.WithTimeout(context.Background(), time.Duration(s3.S3.Timeout)*time.Second)
	defer cancelFn()

	ret = []*Repo{}

	// The directory currently in use is always listed, even before its first sync has written anything. Omitting it
	// would leave the caller's own selection missing from the list it offers the user.
	seen := map[string]bool{}
	addRepo := func(name string) {
		if "" == name || seen[name] {
			return
		}
		seen[name] = true
		ret = append(ret, &Repo{Name: name, Size: 0, Updated: ""})
	}
	if dir := s3.Conf.Dir; "" != dir {
		addRepo(dir)
	} else {
		addRepo(defaultCloudDir)
	}

	// The historical layout keeps the repo at the bucket root and is reported as the default directory.
	if _, statErr := s3.statFile(path.Join("repo", "refs", "latest")); nil == statErr {
		addRepo(defaultCloudDir)
	}

	prefix, delimiter := "siyuan/", "/"
	limit := int32(1000)
	paginator := as3.NewListObjectsV2Paginator(svc, &as3.ListObjectsV2Input{
		Bucket:    aws.String(s3.Conf.S3.Bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String(delimiter),
		MaxKeys:   &limit,
	})
	for paginator.HasMorePages() {
		output, pErr := paginator.NextPage(ctx)
		if nil != pErr {
			err = pErr
			return
		}
		if nil == output {
			break
		}
		for _, commonPrefix := range output.CommonPrefixes {
			if nil == commonPrefix.Prefix {
				continue
			}
			addRepo(strings.Trim(strings.TrimPrefix(*commonPrefix.Prefix, prefix), delimiter))
		}
	}

	sort.Slice(ret, func(i, j int) bool { return ret[i].Name < ret[j].Name })
	return
}

// CreateRepo creates a cloud sync directory.
//
// S3 key prefixes are implicit, so there is nothing to create beyond making the prefix discoverable by listRepos. A
// zero-length marker object does that and is removed again with the directory.
func (s3 *S3) CreateRepo(name string) (err error) {
	if "" == name || defaultCloudDir == name {
		// The default directory is the bucket root, which always exists.
		return
	}
	if !IsValidCloudDirName(name) {
		// Validated here as well as by the caller: this is a library, and the name goes straight into an object key.
		return ErrCloudInvalidDirName
	}

	svc := s3.getService()
	ctx, cancelFn := context.WithTimeout(context.Background(), time.Duration(s3.S3.Timeout)*time.Second)
	defer cancelFn()

	key := path.Join("siyuan", name, ".dejavu-dir")
	_, err = svc.PutObject(ctx, &as3.PutObjectInput{
		Bucket:       aws.String(s3.Conf.S3.Bucket),
		Key:          aws.String(key),
		CacheControl: aws.String("no-cache"),
		Body:         bytes.NewReader(nil),
	})
	return
}

// RemoveRepo removes a cloud sync directory and everything stored under it.
func (s3 *S3) RemoveRepo(name string) (err error) {
	if "" == name || defaultCloudDir == name {
		// Refuse to wipe the bucket root, which is shared with the default directory.
		return ErrCloudInvalidDirName
	}
	if !IsValidCloudDirName(name) {
		// Without this, a name like "." would resolve to the prefix "siyuan/" and take every directory with it.
		return ErrCloudInvalidDirName
	}

	svc := s3.getService()
	prefix := path.Join("siyuan", name) + "/"
	limit := int32(1000)

	// Each request gets its own deadline. A single deadline spanning the whole traversal would expire partway through
	// a large directory and leave it half deleted.
	for {
		var keys []as3Types.ObjectIdentifier
		listCtx, listCancelFn := context.WithTimeout(context.Background(), time.Duration(s3.S3.Timeout)*time.Second)
		output, listErr := svc.ListObjectsV2(listCtx, &as3.ListObjectsV2Input{
			Bucket:  aws.String(s3.Conf.S3.Bucket),
			Prefix:  aws.String(prefix),
			MaxKeys: &limit,
		})
		listCancelFn()
		if nil != listErr {
			return listErr
		}
		if nil == output {
			return
		}
		for _, entry := range output.Contents {
			if nil == entry.Key {
				continue
			}
			keys = append(keys, as3Types.ObjectIdentifier{Key: entry.Key})
		}
		if 1 > len(keys) {
			return
		}

		delCtx, delCancelFn := context.WithTimeout(context.Background(), time.Duration(s3.S3.Timeout)*time.Second)
		delOutput, delErr := svc.DeleteObjects(delCtx, &as3.DeleteObjectsInput{
			Bucket: aws.String(s3.Conf.S3.Bucket),
			Delete: &as3Types.Delete{Objects: keys, Quiet: aws.Bool(true)},
		})
		delCancelFn()
		if nil != delErr {
			return delErr
		}
		if nil != delOutput && 0 < len(delOutput.Errors) {
			first := delOutput.Errors[0]
			return fmt.Errorf("delete cloud dir object [%s] failed: %s", aws.ToString(first.Key), aws.ToString(first.Message))
		}
		// Re-list rather than paginating: the keys just removed are gone, so the next page is simply the new first page.
		// This also terminates only once the prefix is genuinely empty.
	}
}

func (s3 *S3) statFile(key string) (info *objectInfo, err error) {
	svc := s3.getService()
	ctx, cancelFn := context.WithTimeout(context.Background(), time.Duration(s3.S3.Timeout)*time.Second)
	defer cancelFn()

	header, err := svc.HeadObject(ctx, &as3.HeadObjectInput{
		Bucket: &s3.Conf.S3.Bucket,
		Key:    &key,
	})
	if nil != err {
		return
	}

	updated := time.Now().Format("2006-01-02 15:04:05")
	info = &objectInfo{Key: key, Updated: updated, Size: 0}
	if nil == header {
		logging.LogWarnf("stat file [%s] header is nil", key)
		return
	}
	info.Size = *header.ContentLength
	if 1 > info.Size {
		logging.LogWarnf("stat file [%s] size is [%d]", key, info.Size)
	}
	if nil == header.LastModified {
		logging.LogWarnf("stat file [%s] header last modified is nil", key)
	} else {
		updated = header.LastModified.Format("2006-01-02 15:04:05")
	}
	info.Updated = updated
	return
}

func (s3 *S3) getNotFound(keys []string) (ret []string, err error) {
	if 1 > len(keys) {
		return
	}

	poolSize := s3.GetConcurrentReqs()
	if poolSize > len(keys) {
		poolSize = len(keys)
	}

	waitGroup := &sync.WaitGroup{}
	// The workers all append to ret. Without the lock an append can be lost, which reports a chunk that is missing
	// from the cloud as present: it is then never uploaded, and the cloud repo is quietly left incomplete.
	retLock := &sync.Mutex{}
	p, _ := ants.NewPoolWithFunc(poolSize, func(arg interface{}) {
		defer waitGroup.Done()
		key := arg.(string)
		info, statErr := s3.statFile(key)
		if nil == info || nil != statErr {
			retLock.Lock()
			ret = append(ret, key)
			retLock.Unlock()
		}
	})

	for _, key := range keys {
		waitGroup.Add(1)
		err = p.Invoke(key)
		if nil != err {
			logging.LogErrorf("invoke failed: %s", err)
			return
		}
	}
	waitGroup.Wait()
	p.Release()
	return
}

func (s3 *S3) getService() *as3.Client {
	s3.mux.Lock()
	defer s3.mux.Unlock()

	if nil != s3.service {
		return s3.service
	}

	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		logging.LogErrorf("load default config failed: %s", err)
	}

	s3.service = as3.NewFromConfig(cfg, func(o *as3.Options) {
		o.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(s3.Conf.S3.AccessKey, s3.Conf.S3.SecretKey, ""))
		o.BaseEndpoint = aws.String(s3.Conf.S3.Endpoint)
		o.Region = s3.Conf.S3.Region
		o.UsePathStyle = s3.Conf.S3.PathStyle
		o.HTTPClient = s3.HTTPClient
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired

		// --- START: S3 Compatibility Fix for SigV4 (Cloudflare Tunnel/Proxies) ---
		// https://github.com/siyuan-note/siyuan/issues/16199
		// This fix addresses the 'SignatureDoesNotMatch' error encountered when using
		// S3-compatible endpoints proxied through services like Cloudflare Tunnel.
		// Proxies may modify headers (like Accept-Encoding), which invalidates the
		// AWS Signature Version 4 calculation.
		endpoint := strings.ToLower(s3.Conf.S3.Endpoint)

		// Only apply the compatibility middleware if the endpoint is NOT an official AWS S3 endpoint.
		if !strings.Contains(endpoint, "amazonaws.com") {
			// ignoreSigningHeaders and HeadersToIgnore are defined in s3_middleware.go (same package).
			ignoreSigningHeaders(o, HeadersToIgnore)
			// logging.LogDebugf("applied S3 compatibility fix for non-AWS endpoint: %s", s3.Conf.S3.Endpoint)
		}
		// --- END: S3 Compatibility Fix ---
	})
	return s3.service
}

var notFoundMsgs = []string{
	"not found",
	"404",
	"no such file or directory",
	"does not exist",
}

func containsStr(str string, strs []string) bool {
	for _, s := range strs {
		if strings.Contains(str, s) {
			return true
		}
	}
	return false
}

func (s3 *S3) isErrNotFound(err error) bool {
	if nil == err {
		return false
	}

	var nsk *as3Types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}

	var nf *as3Types.NotFound
	if errors.As(err, &nf) {
		return true
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		msg := strings.ToLower(apiErr.ErrorMessage())
		return containsStr(msg, notFoundMsgs)
	}

	msg := strings.ToLower(err.Error())
	return containsStr(msg, notFoundMsgs)
}

// HeadersToIgnore lists headers that frequently cause SignatureDoesNotMatch errors
// when used with S3-compatible providers behind proxies (like Cloudflare Tunnel or GCS).
// These headers are temporarily removed before the SigV4 signing process and restored afterwards.
var HeadersToIgnore = []string{
	"Accept-Encoding", // The primary culprit, often modified by proxies.
	"Amz-Sdk-Invocation-Id",
	"Amz-Sdk-Request",
}

type ignoredHeadersKey struct{}

// ignoreSigningHeaders is a helper to inject middleware that excludes specified headers
// from the Signature Version 4 calculation by temporarily removing them.
// This function should be called only for non-AWS S3 endpoints.
func ignoreSigningHeaders(o *as3.Options, headers []string) {
	o.APIOptions = append(o.APIOptions, func(stack *middleware.Stack) error {
		// 1. Insert ignoreHeaders BEFORE the "Signing" middleware
		if err := stack.Finalize.Insert(ignoreHeaders(headers), "Signing", middleware.Before); err != nil {
			return fmt.Errorf("failed to insert S3CompatIgnoreHeaders: %w", err)
		}

		// 2. Insert restoreIgnored AFTER the "Signing" middleware
		if err := stack.Finalize.Insert(restoreIgnored(), "Signing", middleware.After); err != nil {
			return fmt.Errorf("failed to insert S3CompatRestoreHeaders: %w", err)
		}
		return nil
	})
}

// ignoreHeaders removes specified headers and stores them in context for later restoration.
func ignoreHeaders(headers []string) middleware.FinalizeMiddleware {
	return middleware.FinalizeMiddlewareFunc(
		"S3CompatIgnoreHeaders",
		func(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (out middleware.FinalizeOutput, metadata middleware.Metadata, err error) {
			req, ok := in.Request.(*smithyhttp.Request)
			if !ok {
				return out, metadata, &asSigner.SigningError{Err: errors.New("unexpected request middleware type for ignoreHeaders")}
			}

			// Store removed headers and their values
			ignored := make(map[string]string, len(headers))
			for _, h := range headers {
				// Use canonical form for map key (e.g., "Accept-Encoding")
				// strings.Title is necessary for older Go versions to ensure canonicalization.
				canonicalKey := strings.Title(strings.ToLower(h))
				ignored[canonicalKey] = req.Header.Get(h)
				req.Header.Del(h) // Remove header before signing
			}

			// Store the ignored headers in the context
			ctx = middleware.WithStackValue(ctx, ignoredHeadersKey{}, ignored)
			return next.HandleFinalize(ctx, in)
		},
	)
}

// restoreIgnored retrieves headers from context and restores them to the request
// after the signing (Finalize) and before sending.
func restoreIgnored() middleware.FinalizeMiddleware {
	return middleware.FinalizeMiddlewareFunc(
		"S3CompatRestoreHeaders",
		func(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (out middleware.FinalizeOutput, metadata middleware.Metadata, err error) {
			req, ok := in.Request.(*smithyhttp.Request)
			if !ok {
				return out, metadata, errors.New("unexpected request middleware type for restoreIgnored")
			}

			// Execute the next Handler (which includes signing and the actual network request)
			out, metadata, err = next.HandleFinalize(ctx, in)

			// Retrieve ignored headers from the context
			ignored, _ := middleware.GetStackValue(ctx, ignoredHeadersKey{}).(map[string]string)
			// Restore the headers to the request
			for k, v := range ignored {
				if v != "" {
					req.Header.Set(k, v)
				}
			}
			return out, metadata, err
		},
	)
}
