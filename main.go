package main

import (
	"context"
	"crypto/md5"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/fsnotify/fsnotify"
)

type Profile struct {
	Name     string
	Endpoint string
	Region   string
}

var (
	credFile        string
	serverDir       string
	sourceFile      string
	destURI         string
	recursiveFlag   bool
	profiles        map[string]Profile
	mainDirs        = []string{"incoming_tmp", "incoming", "processing", "failed", "completed"}
	watcher         *fsnotify.Watcher
	processingLock  sync.Mutex
	maxRetries      = 10
	initialBackoff  = 30 * time.Second
	errNotImplemented = errors.New("HEAD request not supported")
	db              *sql.DB
)

func main() {
	parseFlags()
	loadCredentials()
	setupDirectories()
	setupDatabase()

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
		log.Fatalf("Unable to load AWS SDK config: %v", err)
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

func setupDatabase() {
	var err error
	db, err = sql.Open("sqlite3", "flood.db")
	if err != nil {
		log.Fatal(err)
	}

	createTable := `
		CREATE TABLE IF NOT EXISTS file_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			profile TEXT,
			bucket TEXT,
			filepath TEXT,
			retries INTEGER,
			last_retry TIMESTAMP,
			upload_outcome TEXT
		);
	`
	_, err = db.Exec(createTable)
	if err != nil {
		log.Fatal(err)
	}
}

func logRetry(filePath, profileName, bucketName string, retries int, outcome string) {
	stmt, err := db.Prepare("INSERT INTO file_records(profile, bucket, filepath, retries, last_retry, upload_outcome) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(profileName, bucketName, filePath, retries, time.Now(), outcome)
	if err != nil {
		log.Fatal(err)
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

	profileName := parts[0] // Profile
	bucketName := parts[1]  // Bucket

	profile, ok := profiles[profileName]
	if !ok {
		log.Printf("Unknown profile: %s", profileName)
		return
	}

	// Create processing directory for the file
	processingPath := filepath.Join(serverDir, "processing", profileName, bucketName)
	os.MkdirAll(processingPath, 0755)
	os.Rename(path, processingPath)

	// Process the file (upload to S3 etc.)
	processFileWithRetry(processingPath, profile, bucketName, 0)
}

func processFileWithRetry(path string, profile Profile, bucketName string, retryCount int) {
	if retryCount > maxRetries {
		log.Printf("Max retries reached for %s. Moving to failed directory.", path)
		moveToFailed(path)
		logRetry(path, profile.Name, bucketName, retryCount, "failure")
		return
	}

	log.Printf("Uploading %s to S3 for profile %s and bucket %s. Retry attempt: %d\n", path, profile.Name, bucketName, retryCount)

	err := validateBucketExists(profile, bucketName)
	if err != nil {
		log.Printf("Error: %v", err)
		moveToFailed(path)
		logRetry(path, profile.Name, bucketName, retryCount, "failure")
		return
	}

	// Simulate S3 upload
	parts := strings.SplitN(path, string(os.PathSeparator), 4)
	if len(parts) < 4 {
		log.Println("Invalid path for S3 upload")
		return
	}

	key := parts[3] // Object key (rest of the path after profile/bucket)
	err = uploadToS3(path, bucketName, key, profile)
	if err != nil {
		log.Printf("Error uploading to S3: %v\n", err)
		if isTransientError(err) {
			// Retry with exponential backoff and jitter
			backoffDuration := initialBackoff * time.Duration(1<<retryCount)
			jitter := time.Duration(rand.Intn(1000)) * time.Millisecond
			time.Sleep(backoffDuration + jitter)
			processFileWithRetry(path, profile, bucketName, retryCount+1)
		} else {
			moveToFailed(path)
			logRetry(path, profile.Name, bucketName, retryCount, "failure")
		}
		return
	}

	// Move to completed directory
	completedPath := strings.Replace(path, "processing", "completed", 1)
	os.MkdirAll(filepath.Dir(completedPath), 0755)
	os.Rename(path, completedPath)
	logRetry(path, profile.Name, bucketName, retryCount, "success")
}

func isTransientError(err error) bool {
	return strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "connection reset") ||
		strings.Contains(err.Error(), "DNS error")
}

func validateBucketExists(profile Profile, bucketName string) error {
	client := s3.NewFromConfig(getAWSConfig(profile))

	resp, err := client.ListBuckets(context.TODO(), &s3.ListBucketsInput{})
	if err != nil {
		return fmt.Errorf("failed to list buckets: %w", err)
	}

	for _, bucket := range resp.Buckets {
		if *bucket.Name == bucketName {
			return nil
		}
	}
	return fmt.Errorf("bucket %s does not exist on S3 server", bucketName)
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

	// Extract profile, bucket, and object key from S3 URI
	profileName, bucketName, objectKey := parts[0], parts[1], parts[2]
	profile, ok := profiles[profileName]
	if !ok {
		log.Fatalf("Unknown profile: %s", profileName)
	}

	// Ensure bucket exists on S3 server
	err := validateBucketExists(profile, bucketName)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	// Create the necessary bucket directory structure in incoming_tmp
	tmpDir := filepath.Join(serverDir, "incoming_tmp", profileName, bucketName)
	os.MkdirAll(tmpDir, 0755)

	// Copy the source file or directory to incoming_tmp
	if recursiveFlag && isDirectory(sourceFile) {
		copyDirectory(sourceFile, filepath.Join(tmpDir, objectKey))
	} else {
		copyFile(sourceFile, filepath.Join(tmpDir, objectKey))
	}

	// Move files from incoming_tmp to incoming (bucket structure must also exist here)
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
	client := s3.NewFromConfig(getAWSConfig(profile))

	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", file, err)
	}
	defer f.Close()

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

func getAWSConfig(profile Profile) aws.Config {
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
		log.Fatalf("failed to load configuration: %v", err)
	}
	return cfg
}
