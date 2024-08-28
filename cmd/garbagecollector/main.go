package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"github.com/juju/clock"
	"github.com/juju/mutex/v2"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"io"
	"os"
	"strings"
	"time"
	"tizbac/pmoxs3backuproxy/internal/s3backuplog"
	"tizbac/pmoxs3backuproxy/internal/s3pmoxcommon"
)

func compareSum(csum []byte, index []byte, metadatasum string) error {
	fileChecksum := hex.EncodeToString(csum)
	shaSum := sha256.Sum256(index)
	checksum := hex.EncodeToString(shaSum[:])

	if fileChecksum != checksum || fileChecksum != metadatasum {
		return errors.New(
			fmt.Sprintf(
				"Corrupted index file: Checksum in index [%s] or metadata sum [%s] does not match calculated checksum [%s]",
				fileChecksum,
				metadatasum,
				checksum,
			),
		)
	}

	return nil
}

func main() {
	endpointFlag := flag.String("endpoint", "", "S3 Endpoint without https/http , host:port")
	secureFlag := flag.Bool("usessl", false, "Use SSL for endpoint connection: default: false")
	bucketFlag := flag.String("bucket", "", "Bucket to perform garbage collection on")
	accessKeyID := flag.String("accesskey", "", "S3 Access Key ID")
	secretKey := flag.String("secretkey", "", "S3 Secret Key, discouraged , use a file if possible")
	secretKeyFile := flag.String("secretkeyfile", "", "S3 Secret Key File")
	retentionDays := flag.Uint("retention", 60, "Number of days to keep backups for")
	lookupTypeFlag := flag.String("lookuptype", "auto", "Bucket lookup type: auto,dns,path")
	debug := flag.Bool("debug", false, "Debug logging")
	flag.Parse()
	if *endpointFlag == "" || *accessKeyID == "" || (*secretKey == "" && *secretKeyFile == "") || *bucketFlag == "" {
		flag.Usage()
		os.Exit(1)
	}
	if *debug {
		s3backuplog.EnableDebug()
	}

	skey := *secretKey
	if skey == "" {
		data, err := os.ReadFile(*secretKeyFile)
		if err != nil {
			s3backuplog.ErrorPrint("Reading key file %s : %s", *secretKeyFile, err.Error())
			os.Exit(1)
		}
		skey = string(data)
		skey = strings.Trim(skey, " \r\t\n")
	}

	var err error
	minioClient, minioerr := minio.New(*endpointFlag, &minio.Options{
		Creds:        credentials.NewStaticV4(*accessKeyID, skey, ""),
		Secure:       (*secureFlag),
		BucketLookup: s3pmoxcommon.GetLookupType(*lookupTypeFlag),
	})
	if minioerr != nil {
		s3backuplog.ErrorPrint("Creating S3 Client: %s", minioerr.Error())
		os.Exit(1)
	}

	s3backuplog.InfoPrint("Acquire Lock")
	h := sha256.Sum256([]byte(*endpointFlag + "|" + *bucketFlag))
	lockname := "PBSS3" + hex.EncodeToString(h[:])[:16]
	sp := mutex.Spec{
		Clock:   clock.WallClock,
		Name:    lockname,
		Delay:   time.Millisecond,
		Timeout: time.Second * 30,
	}

	SessionsRelease, err := mutex.Acquire(sp)
	if err != nil {
		s3backuplog.ErrorPrint("Failed to acquire Lock for %s: %s", lockname, err.Error())
		os.Exit(1)
	}
	s3backuplog.DebugPrint("Locked %s", lockname)

	//TODO Locking

	ctx := context.Background()
	bucket, staterr := minioClient.BucketExists(ctx, *bucketFlag)
	if staterr != nil {
		s3backuplog.ErrorPrint("Unable to access specified bucket: %s", staterr.Error())
		os.Exit(1)
	}
	if !bucket {
		s3backuplog.ErrorPrint("Specified bucket [%s] does not exist", *bucketFlag)
		os.Exit(1)

	}

	//Phase 1 Delete backups older than retentionDays
	s3backuplog.InfoPrint("Fetching snapshots")
	snapshots, err := s3pmoxcommon.ListSnapshots(*minioClient, *bucketFlag, true)
	if err != nil {
		s3backuplog.ErrorPrint("Unable to list snapshots: %s", err.Error())
		os.Exit(1)
	}

	if len(snapshots) == 0 {
		s3backuplog.WarnPrint("No snapshots found in bucket")
		os.Exit(1)
	}

	s3backuplog.InfoPrint("%v snapshots in bucket", len(snapshots))
	for _, s := range snapshots {
		if s.BackupTime+(uint64(*retentionDays))*86400 < uint64(time.Now().Unix()) {
			if s.Protected == true {
				s3backuplog.InfoPrint("Backup %s,%s/%d is older than %d but marked as protected, skip removal.",
					s.S3Prefix,
					s.BackupID,
					s.BackupTime,
					*retentionDays,
				)
				continue
			}
			s3backuplog.InfoPrint("Backup %s is older than %d days, deleting", s.S3Prefix(), *retentionDays)
			s.Delete(*minioClient)
		} else {
			s3backuplog.InfoPrint("Backup %s is newer than %d days, keeping", s.S3Prefix(), *retentionDays)
		}
	}

	//Phase 2 Figure out which objects under indexed/ are orphaned and delete them

	knownHashes := make(map[string]bool)
	knownChunks := make(map[string][]string)
	existingChunks := make(map[string]bool)
	s3backuplog.InfoPrint("Fetching object hashes")
	for object := range minioClient.ListObjects(ctx, *bucketFlag, minio.ListObjectsOptions{
		Recursive:    true,
		Prefix:       "backups/",
		WithMetadata: true,
	}) {
		// indexed folder only set for fixed index snapshots
		if !strings.HasSuffix(object.Key, ".fidx") {
			continue
		}
		if object.UserMetadata["X-Amz-Meta-Csum"] == "" {
			s3backuplog.ErrorPrint("%s: object has no csum metadata flag set", object.Key)
			os.Exit(1)
		}
		knownHashes[object.UserMetadata["X-Amz-Meta-Csum"]] = true
	}
	s3backuplog.InfoPrint("%v object hashes found", len(knownHashes))

	s3backuplog.InfoPrint("Removing orphaned object hashes")
	objectsCh := make(chan minio.ObjectInfo)
	go func() {
		defer close(objectsCh)
		for object := range minioClient.ListObjects(ctx, *bucketFlag, minio.ListObjectsOptions{
			Recursive:    true,
			Prefix:       "indexed/",
			WithMetadata: true,
		}) {
			_, ok := knownHashes[object.UserMetadata["X-Amz-Meta-Csum"]]
			if !ok {
				s3backuplog.InfoPrint("Removing orphaned object hash %s for object %s", object.UserMetadata["X-Amz-Meta-Csum"], object.Key)
				objectsCh <- object
			}
		}
	}()

	errorCh := minioClient.RemoveObjects(context.Background(), *bucketFlag, objectsCh, minio.RemoveObjectsOptions{})
	for e := range errorCh {
		s3backuplog.ErrorPrint("Failed to remove " + e.ObjectName + ", error: " + e.Err.Error())
	}
	//Phase 3 Mark Used chunks
	for object := range minioClient.ListObjects(ctx, *bucketFlag, minio.ListObjectsOptions{
		Recursive:    true,
		Prefix:       "backups/",
		WithMetadata: true,
	}) {
		if strings.HasSuffix(object.Key, ".fidx") {
			s3backuplog.InfoPrint("Processing fixed index: %s", object.Key)
			o, err := minioClient.GetObject(ctx, *bucketFlag, object.Key, minio.GetObjectOptions{})
			if err != nil {
				s3backuplog.ErrorPrint("Error accessing object %s: %s", object.Key, err.Error())
				os.Exit(1)
			}
			data, err := io.ReadAll(o)
			if err != nil {
				s3backuplog.ErrorPrint("Error reading object %s: %s", object.Key, err.Error())
				os.Exit(1)
			}
			if len(data) < 4096 {
				s3backuplog.ErrorPrint("Error reading object %s: Too small", object.Key)
				os.Exit(1)
			}

			if csumerr := compareSum(data[32:64], data[4096:], object.UserMetadata["X-Amz-Meta-Csum"]); csumerr != nil {
				s3backuplog.ErrorPrint("%s", csumerr.Error())
				os.Exit(1)
			}

			data = data[4096:]
			if len(data)%32 != 0 {
				s3backuplog.ErrorPrint("Error examining object %s: Data after header length is not 32 bytes aligned", object.Key)
				os.Exit(1)
			}
			for i := 0; i < len(data)/32; i++ {
				val, ok := knownChunks[hex.EncodeToString(data[i*32:(i+1)*32])]
				if !ok {
					val = make([]string, 0)
				}
				val = append(val, object.Key)
				knownChunks[hex.EncodeToString(data[i*32:(i+1)*32])] = val
			}
		}
		if strings.HasSuffix(object.Key, ".didx") {
			s3backuplog.InfoPrint("Processing dynamic index: %s", object.Key)
			o, err := minioClient.GetObject(ctx, *bucketFlag, object.Key, minio.GetObjectOptions{})
			if err != nil {
				s3backuplog.ErrorPrint("Error accessing object %s: %s", object.Key, err.Error())
				os.Exit(1)
			}
			data, err := io.ReadAll(o)
			if err != nil {
				s3backuplog.ErrorPrint("Error reading object %s: %s", object.Key, err.Error())
				os.Exit(1)
			}
			if len(data) < 4096 {
				s3backuplog.ErrorPrint("Error reading object %s: Too small", object.Key)
				os.Exit(1)
			}

			if csumerr := compareSum(data[32:64], data[4096:], object.UserMetadata["X-Amz-Meta-Csum"]); csumerr != nil {
				s3backuplog.ErrorPrint("%s", csumerr.Error())
				os.Exit(1)
			}

			reader := bytes.NewReader(data[4096:])
			var offset int64 = 0
			for {
				var chunk_offset = make([]byte, 8)
				var digest_offset = make([]byte, 32)
				reader.ReadAt(chunk_offset, offset)
				offset += 8
				reader.ReadAt(digest_offset, offset)
				offset += 32
				chunk_off := binary.LittleEndian.Uint64(chunk_offset)
				s3backuplog.DebugPrint("Offset: %d", uint64(chunk_off))
				val := hex.EncodeToString(digest_offset)
				s3backuplog.DebugPrint("Digest: %s", val)
				known, ok := knownChunks[val]
				if !ok {
					known = make([]string, 0)
				}
				known = append(known, object.Key)
				knownChunks[val] = known

				if offset == int64(reader.Len()) {
					break
				}
			}
		}
	}

	s3backuplog.InfoPrint("Enumerated %d referenced chunks", len(knownChunks))
	//Delete orphaned chunks

	objectsCh = make(chan minio.ObjectInfo)
	go func() {
		defer close(objectsCh)
		for object := range minioClient.ListObjects(ctx, *bucketFlag, minio.ListObjectsOptions{Recursive: true, Prefix: "chunks/"}) {
			chunkhash := strings.ReplaceAll(object.Key[7:], "/", "")
			_, ok := knownChunks[chunkhash]
			if !ok {
				objectsCh <- object
			} else {
				s3backuplog.DebugPrint("Chunk still referenced: %s, skip removal", chunkhash)
				existingChunks[chunkhash] = true
			}
		}
	}()

	s3backuplog.InfoPrint("Removing orphaned chunks")
	errorCh = minioClient.RemoveObjects(context.Background(), *bucketFlag, objectsCh, minio.RemoveObjectsOptions{})
	for e := range errorCh {
		s3backuplog.ErrorPrint("Failed to remove " + e.ObjectName + ", error: " + e.Err.Error())
	}
	// Do an integrity check to ensure that all referenced chunks exist
	s3backuplog.InfoPrint("Running integrity check")
	for k, v := range knownChunks {
		_, ok := existingChunks[k]
		if !ok {
			s3backuplog.ErrorPrint("Corruption detected, chunk %s, referenced by %s is missing!!", k, strings.Join(v, ","))
			//We mark the backup corrupted to allow new backup to skip incremental and recreate missing chunks
			for _, oname := range v {
				basepatht := strings.Split(oname, "/")
				basepatht = basepatht[0 : len(basepatht)-1]
				basepath := strings.Join(basepatht, "/")
				r := strings.NewReader("CORRUPTED")
				_, err := minioClient.PutObject(ctx, *bucketFlag, basepath+"/corrupted", r, 9, minio.PutObjectOptions{})
				if err != nil {
					s3backuplog.ErrorPrint("Error tagging %s as corrupt: %s", oname, err.Error())
					os.Exit(1)
				}
			}

		}
	}
	s3backuplog.InfoPrint("Finished")
	SessionsRelease.Release()
}
