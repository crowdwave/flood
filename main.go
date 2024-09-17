package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/fsnotify/fsnotify"
)

type Profile struct {
	Name     string
	Endpoint string
	Region   string
}

var (
	credFile       string
	serverDir      string
	sourceFile     string
	destURI        string
	recursiveFlag  bool
	profiles       map[string]Profile
	mainDirs       = []string{"incoming_tmp", "incoming", "processing", "failed", "completed"}
	watcher        *fsnotify.Watcher
	processingLock sync.Mutex
	errNotImplemented = errors.New("HEAD request not supported")
)

func main() {
	parseFlags()
	loadCredentials()
	setupDirectories()

	if serverDir != "" {
		runServerMode()
	} else if sourceFile != "" && destURI != "" {
		runCopyMode()
	} else {
		log.Fatal("Invalid mode. Specify either server directory or source file and destination URI.")
	}
}

func parseFlags() {
	flag.StringVar(&credFile, "cred", "", "Path to credentials file")
	flag.StringVar(&serverDir, "server", "", "Server directory")
	flag.StringVar(&sourceFile, "source", "", "Source file or directory")
	flag.StringVar(&destURI, "dest", "", "Destination S3 URI")
	flag.BoolVar(&recursiveFlag, "r", false, "Recursive copy")
	flag.Parse()

	if credFile == "" {
		credFile = findCredentials()
	}
}

func findCredentials() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".aws", "credentials")
}

func loadCredentials() {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("unable to load AWS SDK config, %v", err)
	}

	profiles = make(map[string]Profile)
	for profileName, profileCfg := range cfg.SharedConfigProfiles {
		profiles[profileName] = Profile{
			Name:     profileName,
			Endpoint: profileCfg.Endpoint,
			Region:   profileCfg.Region,
		}
	}
}

func setupDirectories() {
	if serverDir != "" {
		os.RemoveAll(filepath.Join(serverDir, "incoming_tmp"))
		for _, dir := range mainDirs {
			for profile := range profiles {
				os.MkdirAll(filepath.Join(serverDir, dir, profile), 0755)
			}
		}
	}
}

func runServerMode() {
	processExistingFiles()
	setupWatcher()
	processIncomingFiles()
}

func processExistingFiles() {
	for _, profile := range profiles {
		processDir := filepath.Join(serverDir, "processing", profile.Name)
		filepath.Walk(processDir, func(path string, info os.FileInfo, err error) error {
			if !info.IsDir() {
				processFile(path, profile)
			}
			return nil
		})
	}
}

func setupWatcher() {
	var err error
	watcher, err = fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.CloseWrite == fsnotify.CloseWrite ||
					event.Op&fsnotify.Create == fsnotify.Create {
					handleFileEvent(event.Name)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
			}
		}
	}()

	err = filepath.Walk(filepath.Join(serverDir, "incoming"), func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			err = watcher.Add(path)
			if err != nil {
				log.Fatal(err)
			}
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}

func handleFileEvent(path string) {
	processingLock.Lock()
	defer processingLock.Unlock()

	relativePath, _ := filepath.Rel(filepath.Join(serverDir, "incoming"), path)
	parts := strings.SplitN(relativePath, string(os.PathSeparator), 3)
	if len(parts) < 2 {
		return
	}

	profile, ok := profiles[parts[0]]
	if !ok {
		log.Printf("Unknown profile: %s", parts[0])
		return
	}

	processingPath := filepath.Join(serverDir, "processing", relativePath)
	os.MkdirAll(filepath.Dir(processingPath), 0755)
	os.Rename(path, processingPath)

	processFile(processingPath, profile)
}

func processFile(path string, profile Profile) {
	log.Printf("Uploading %s to S3 for profile %s\n", path, profile.Name)

	parts := strings.SplitN(path, string(os.PathSeparator), 4)
	if len(parts) < 4 {
		log.Println("Invalid path for S3 upload")
		return
	}

	bucket, key := parts[2], parts[3]
	err := uploadToS3(path, bucket, key, profile)
	if err != nil {
		log.Printf("Error uploading to S3: %v\n", err)
		moveToFailed(path)
		return
	}

	completedPath := strings.Replace(path, "processing", "completed", 1)
	os.MkdirAll(filepath.Dir(completedPath), 0755)
	os.Rename(path, completedPath)
}

func processIncomingFiles() {
	for _, profile := range profiles {
		incomingDir := filepath.Join(serverDir, "incoming", profile.Name)
		filepath.Walk(incomingDir, func(path string, info os.FileInfo, err error) error {
			if !info.IsDir() {
				handleFileEvent(path)
			}
			return nil
		})
	}
}

func runCopyMode() {
	parts := strings.SplitN(strings.TrimPrefix(destURI, "s3://"), "/", 3)
	if len(parts) < 3 {
		log.Fatal("Invalid S3 URI")
	}

	profileName, bucketName, objectKey := parts[0], parts[1], parts[2]
	profile, ok := profiles[profileName]
	if !ok {
		log.Fatalf("Unknown profile: %s", profileName)
	}

	tmpDir := filepath.Join(serverDir, "incoming_tmp", profileName, bucketName)
	os.MkdirAll(tmpDir, 0755)

	if recursiveFlag && isDirectory(sourceFile) {
		copyDirectory(sourceFile, filepath.Join(tmpDir, objectKey))
	} else {
		copyFile(sourceFile, filepath.Join(tmpDir, objectKey))
	}

	moveToIncoming(tmpDir, profileName, bucketName)
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		log.Fatal(err)
	}
	return info.IsDir()
}

func copyDirectory(src, dst string) {
	filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(src, path)
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			os.MkdirAll(dstPath, info.Mode())
		} else {
			copyFile(path, dstPath)
		}
		return nil
	})
}

func copyFile(src, dst string) {
	input, err := os.ReadFile(src)
	if err != nil {
		log.Fatal(err)
	}

	err = os.MkdirAll(filepath.Dir(dst), 0755)
	if err != nil {
		log.Fatal(err)
	}

	err = os.WriteFile(dst, input, 0644)
	if err != nil {
		log.Fatal(err)
	}
}

func moveToIncoming(tmpDir, profileName, bucketName string) {
	incomingDir := filepath.Join(serverDir, "incoming", profileName, bucketName)
	os.MkdirAll(incomingDir, 0755)

	filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(tmpDir, path)
		dstPath := filepath.Join(incomingDir, relPath)

		if info.IsDir() {
			os.MkdirAll(dstPath, info.Mode())
		} else {
			os.Rename(path, dstPath)
		}
		return nil
	})

	os.RemoveAll(tmpDir)
}

func moveToFailed(path string) {
	failedPath := strings.Replace(path, "processing", "failed", 1)
	os.MkdirAll(filepath.Dir(failedPath), 0755)
	os.Rename(path, failedPath)
}

func uploadToS3(file, bucket, key string, profile Profile) error {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(profile.Region),
		config.WithEndpointResolverWithOptions(
			aws.EndpointResolverWithOptionsFunc(
				func(service, region string, options ...interface{}) (aws.Endpoint, error) {
					return aws.Endpoint{URL: profile.Endpoint}, nil
				},
			),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	client := s3.NewFromConfig(cfg)

	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", file, err)
	}
	defer f.Close()

	exists, err := checkFileExistsInS3(client, bucket, key)
	if err != nil && !errors.Is(err, errNotImplemented) {
		return fmt.Errorf("failed to check if file exists: %w", err)
	}
	if exists {
		log.Printf("File %s already exists in bucket %s with key %s, skipping upload", file, bucket, key)
		return nil
	}

	_, err = client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   f,
	})
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}

	return nil
}

func checkFileExistsInS3(client *s3.Client, bucket, key string) (bool, error) {
	resp, err := client.HeadObject(context.TODO(), &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var apiErr *types.NotImplemented
		if errors.As(err, &apiErr) {
			log.Println("HEAD request not supported by server, proceeding with upload")
			return false, errNotImplemented
		}
		return false, fmt.Errorf("failed to perform HEAD request: %w", err)
	}

	etag := strings.Trim(resp.ETag, "\"")

	fileMD5, err := computeMD5Hash(file)
	if err != nil {
		return false, fmt.Errorf("failed to compute MD5 hash of the file: %w", err)
	}

	if etag == fileMD5 {
		return true, nil
	}

	return false, nil
}

func computeMD5Hash(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", fmt.Errorf("failed to open file for MD5 computation: %w", err)
	}
	defer f.Close()

	hasher := md5.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", fmt.Errorf("failed to hash file contents: %w", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
