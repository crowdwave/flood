### **Flood: Complete Program Requirements**

**Flood** is a dual-mode program designed to automate the transfer of files to S3-compatible cloud storage providers, specifically **Cloudflare** and **Backblaze**, via their S3 compatibility APIs. It can operate in:
- **Server mode**, where it continuously monitors directories for incoming files, processes them, and transfers them to the appropriate S3 buckets.
- **Copy mode**, where it accepts individual files or directories and transfers them to a specified S3 URI.

**Flood** dynamically manages bucket directories and ensures that files are only copied into existing buckets on the S3 server. The program outputs detailed logs for every action it performs, including file movements and S3 uploads.

---

### Final Numbered Requirements for **Flood**:

#### General:
1. The program must accept a credentials file (with AWS credentials syntax) as an argument.
2. It must parse the AWS credentials file and extract all profiles, including named profiles.
3. It must validate the credentials file to ensure it has at least one profile.
4. Each profile **must supply an AWS_ENDPOINT** and **AWS_REGION** (either directly or inherited via `source_profile`).
5. For each profile, the program must ensure the creation of subdirectories under five main directories: `incoming_tmp`, `incoming`, `processing`, `failed`, and `completed`.

#### File Structure:
6. The **bucket name** is the second directory level under each profile in the main directories (`incoming_tmp`, `incoming`, `processing`, `failed`, and `completed`).
   - Directory structure:
     ```
     /server_directory/{main_dir}/{profileName}/{bucketName}/...
     ```
   - Example:
     ```
     /server_directory/incoming/profile1/mybucket/file.txt
     ```
7. **Profile and bucket name derivation**:
   - In **server mode**, the bucket name is derived from the directory path as the second level under each profile when processing files in directories like `incoming` and `processing`.
   - In **copy mode**, the bucket name is extracted from the S3 URI (e.g., `s3://profile1/mybucket/file.txt`), where the second component (`mybucket`) is treated as the bucket name.

#### Server Mode:
8. The program must be able to run in **server mode** if a `server_directory` argument is provided.
9. In **server mode**, the program should run continuously, processing files as they arrive in `incoming`.
10. The program must use `fsnotify` to monitor the `incoming` directory for `MOVE` or `CLOSE_WRITE` events.
11. A file is considered to have arrived if it triggers a `MOVE` or `CLOSE_WRITE` event in the `incoming` directory.
12. **Recursive directory watching**: The program must watch subdirectories inside the `incoming` directory and process files within them.
13. Once detected, files must be moved to the corresponding profile's directory under `processing` for handling.
14. Files should then be moved to either `completed` (if processing succeeds) or `failed` (if processing fails).
15. In **server mode**, after reading the credentials file, all directories (from requirement 5) must be created under the `server_directory`.
16. In **server mode**, the credentials file must still be provided and parsed as per requirement 1.
17. The core purpose of the program in server mode is to move files to S3 buckets based on the profile and bucket structure in the `incoming` directory.
18. Before enabling `fsnotify` monitoring, the program must first **process all existing files** in the `processing` directory for each profile.
19. After processing files in the `processing` directory, it must then process all existing files in the `incoming` directory for each profile.
20. Only after all existing files have been processed from `processing` and `incoming` should the program enable `fsnotify` to monitor new files in `incoming`.

#### Copy Mode:
21. The program must also support **copy mode**, where it accepts a **source directory or filename** and copies it to the appropriate profile's `incoming_tmp` directory.
22. In **copy mode**, the destination must be specified in the format: `s3://{profilename}/{bucketname}/filepath_or_name`.
23. The program must extract the **profile** and **bucket** directly from the S3 URI.
24. The program must **validate the profile** and **bucket** by checking the credentials file and the structure of the `incoming` and `incoming_tmp` directories.
25. The program must dynamically create bucket directories (if they don't exist) during **copy mode** operations.
   - The necessary bucket directory structure must be created in both `incoming_tmp` and `incoming` as needed.
26. In **copy mode**, the program must support an **optional recursive copy** (using typical command-line syntax such as `-r` or `--recursive`).
    - If the recursive flag is used, it should copy directories and all their contents to `incoming_tmp`.
27. After the file or directory is copied into `incoming_tmp`, it must be **moved** to the corresponding location in `incoming`.
28. If a directory or file is copied, it must respect the profile and S3 bucket structure, meaning files are placed in the correct subdirectory based on the profile and bucket specified in the S3 URI.

#### Directory Handling:
29. An `incoming_tmp` directory with the same subdirectory structure as `incoming` must be created, where files are initially copied.
30. All new files must be first copied into the appropriate subdirectory of `incoming_tmp` for each profile and bucket.
31. Once a file copy is complete in `incoming_tmp`, the file should be moved to the corresponding location in the `incoming` directory, which is monitored by `fsnotify`.
32. When the program starts, it must first delete the **entire contents** of the `incoming_tmp` directory to ensure it is empty before continuing.
33. After deleting the contents of `incoming_tmp`, the program must proceed with creating all required directories for profiles and buckets under `incoming_tmp`, `incoming`, `processing`, `failed`, and `completed`.

#### File Processing:
34. Files are processed by **copying them to the target S3 profile** using the **Golang AWS SDK**.
35. The program must ensure that the file is fully uploaded to the S3 bucket before moving it to the `completed` directory.
36. If the S3 upload fails, the program must log the error and move the file to the `failed` directory for retry or manual intervention.

#### Bucket Validation Against S3 Server:
37. Before copying any files to a bucket, the program must **validate the existence of the bucket on the S3 server**.
   - The program must:
     1. Query the S3 server for a list of buckets using the AWS SDK's `ListBuckets` function.
     2. Ensure that the bucket specified in the S3 URI (or derived from the directory structure) exists on the server.
   - Example:
     ```go
     client.ListBuckets(context.TODO(), &s3.ListBucketsInput{})
     ```
38. If the bucket does not exist on the S3 server, the program should:
   - Log an error indicating that the bucket does not exist.
   - Skip the operation for that specific bucket.
   - Example:
     ```
     Error: Bucket 'mybucket' does not exist on S3 server.
     ```

#### Credentials Search:
39. The program must search for the AWS credentials file using the **same algorithm** as the AWS CLI, before falling back to the file provided as an argument (see the AWS credentials search algorithm below).

#### Logging and Output:
40. The program must output **detailed logging** and **information** about each action it performs, including file movements, copying, and S3 uploads.

---

### AWS S3 CLI Credentials File Search Algorithm:

1. **Environment Variables**:
   - The AWS CLI first checks for credentials in the following environment variables:
     - `AWS_ACCESS_KEY_ID`
     - `AWS_SECRET_ACCESS_KEY`
     - `AWS_SESSION_TOKEN` (if using temporary credentials)
     - If these environment variables are set, they are used, and no further search occurs for the credentials file.

2. **Shared Credentials File**:
   - If the environment variables are not set, the CLI looks for the credentials file in the **default location**:
     - **Linux/macOS**: `~/.aws/credentials`
     - **Windows**: `C:\Users\USERNAME\.aws\credentials`
   - The location of the credentials file can be overridden by the `AWS_SHARED_CREDENTIALS_FILE` environment variable.

3. **Config File**:
   - If the credentials file is not found or doesn’t contain the needed values, the CLI will also check the configuration file (`~/.aws/config`), which may have `[profile]` entries containing `aws_access_key_id` and `aws_secret_access_key`. This file can also provide default values for `AWS_REGION`.

4. **Profile Option**:
   - If a profile is explicitly specified using the `--profile` command-line option or the `AWS_PROFILE` environment variable, the CLI searches for the corresponding profile in the credentials file (`~/.aws/credentials`) or configuration file (`~/.aws/config`).

---

### Example Flow:
1. A user attempts to copy a file with the S3 URI `s3://profile1/mybucket/file.txt`.
2. The program extracts `profile1` as the profile name and `mybucket` as the bucket name.
3. The program queries the S3 server to ensure that `mybucket` exists for `profile1`.
4. If the bucket exists:
   - The file is first copied into `incoming_tmp/profile1/mybucket/`.
  

 - After the copy is complete, the file is moved to `incoming/profile1/mybucket/`.
5. If the bucket does not exist:
   - The program logs an error and skips the copy operation.

---

### Summary of Key Points:
- The program dynamically manages the creation of bucket directories based on the S3 URI structure or directory structure.
- **Bucket validation** is performed against the S3 server before any file copy operation to ensure that the bucket exists.
- Detailed logging and error handling ensure that invalid operations are reported, and invalid buckets are skipped.

Let me know if you'd like to proceed with the implementation or if any other clarifications are needed!
