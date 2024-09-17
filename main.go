### Updated Requirements for **Flood** with Retry Mechanism

The **Flood** program now includes a robust retry mechanism with **exponential backoff** starting at 30 seconds and increasing up to a maximum of 10 retries. This ensures that the program can handle temporary S3 provider unavailability effectively while avoiding overwhelming the S3 service with frequent retries.

---

### New Retry Mechanism Requirements:

#### Retry Mechanism Requirements:
1. **Exponential Backoff with Jitter**:
   - Implement **exponential backoff with jitter** for retries on transient S3 upload failures.
   - The initial backoff delay is **30 seconds**, and it doubles with each retry attempt (30s, 60s, 120s, etc.), with a random jitter added to each delay to avoid synchronization issues.
   - The backoff will continue for up to **10 retry attempts**.

2. **Maximum Retry Count**:
   - The program must retry up to a **maximum of 10 times** for each file.
   - After reaching the maximum retry limit (i.e., after 10 retries), the file should be marked as **failed** and moved to the `failed` directory.

3. **Retry on Specific Errors**:
   - The retry mechanism will apply only for **transient errors** such as:
     - Network timeouts
     - Connection resets
     - DNS errors
   - Non-recoverable errors (e.g., missing bucket, authentication failure) will not trigger a retry.

4. **Track Retry Attempts in SQLite**:
   - Each retry attempt must be logged in the SQLite database, recording:
     - The number of retries attempted
     - The timestamp of each retry
     - The outcome of the retry (e.g., success or failure)

5. **Backoff Reset on Success**:
   - Once a file is successfully uploaded, the retry counter must be reset, and no further retries will be attempted for that file.

---

### Updated and Complete **Flood** Program Requirements:

#### General:
1. The program must accept a credentials file (with AWS credentials syntax) as an argument.
2. It must parse the AWS credentials file and extract all profiles, including named profiles.
3. It must validate the credentials file to ensure it has at least one profile.
4. **Each profile must supply the following based on the `provider`**:
   - If `provider = amazon`:
     - `AWS_ACCESS_KEY_ID`
     - `AWS_SECRET_ACCESS_KEY`
     - `AWS_REGION`
   - If `provider = cloudflare`:
     - `AWS_ACCESS_KEY_ID`
     - `AWS_SECRET_ACCESS_KEY`
     - `AWS_REGION`
     - `AWS_ENDPOINT`
   - If `provider = backblaze`:
     - `AWS_ACCESS_KEY_ID`
     - `AWS_SECRET_ACCESS_KEY`
     - `AWS_REGION`
     - `AWS_ENDPOINT`
5. If the **`provider` key is missing**, the program must log an error and **abort execution** with a message indicating the missing provider.
6. If any required configuration key is missing based on the provider, the program must log an error and **abort execution** with a message indicating the missing key(s).

#### Retry Mechanism:
7. The program must include a **retry mechanism** with **exponential backoff and jitter** for handling transient errors when attempting to upload files to S3.
   - The retry mechanism will have an initial delay of **30 seconds**, which doubles with each retry attempt (30s, 60s, 120s, etc.).
   - A random jitter will be added to each delay to prevent synchronized retries across multiple clients.
   - The program must retry up to a **maximum of 10 attempts** for each file.
8. The retry mechanism must apply only to **transient errors**, including:
   - Network timeouts
   - DNS failures
   - Connection resets
9. **Non-recoverable errors** (e.g., authentication failure, missing bucket) must not trigger retries.
10. Each retry attempt must be logged in the **SQLite database**, including:
    - The number of retries attempted
    - The timestamp of each retry
    - The outcome of the retry (success or failure).
11. If a file is successfully uploaded after a retry, the backoff and retry counter must be **reset**.
12. After **10 failed retry attempts**, the program must log the failure, move the file to the **`failed` directory**, and update the database accordingly.

#### File Structure:
13. The **bucket name** is the second directory level under each profile in the main directories (`incoming_tmp`, `incoming`, `processing`, `failed`, and `completed`).
   - Directory structure:
     ```
     /server_directory/{main_dir}/{profileName}/{bucketName}/...
     ```
   - Example:
     ```
     /server_directory/incoming/profile1/mybucket/file.txt
     ```

#### Server Mode:
14. The program must be able to run in **server mode** if a `server_directory` argument is provided.
15. In **server mode**, the program should run continuously, processing files as they arrive in `incoming`.
16. The program must use `fsnotify` to monitor the `incoming` directory for `MOVE` or `CLOSE_WRITE` events.
17. A file is considered to have arrived if it triggers a `MOVE` or `CLOSE_WRITE` event in the `incoming` directory.
18. **Recursive directory watching**: The program must watch subdirectories inside the `incoming` directory and process files within them.
19. Once detected, files must be moved to the corresponding profile's directory under `processing` for handling.
20. Files should then be moved to either `completed` (if processing succeeds) or `failed` (if processing fails).
21. In **server mode**, after reading the credentials file, all directories (from requirement 13) must be created under the `server_directory`.
22. In **server mode**, the credentials file must still be provided and parsed as per requirement 1.
23. The core purpose of the program in server mode is to move files to S3 buckets based on the profile and bucket structure in the `incoming` directory.
24. Before enabling `fsnotify` monitoring, the program must first **process all existing files** in the `processing` directory for each profile.
25. After processing files in the `processing` directory, it must then process all existing files in the `incoming` directory for each profile.
26. Only after all existing files have been processed from `processing` and `incoming` should the program enable `fsnotify` to monitor new files in `incoming`.

#### Copy Mode:
27. The program must also support **copy mode**, where it accepts a **source directory or filename** and copies it to the appropriate profile's `incoming_tmp` directory.
28. In **copy mode**, the destination must be specified in the format: `s3://{profilename}/{bucketname}/filepath_or_name`.
29. The program must extract the **profile** and **bucket** directly from the S3 URI.
30. The program must **validate the profile** and **bucket** by checking the credentials file and the structure of the `incoming` and `incoming_tmp` directories.
31. The program must dynamically create bucket directories (if they don't exist) during **copy mode** operations.
   - The necessary bucket directory structure must be created in both `incoming_tmp` and `incoming` as needed.
32. In **copy mode**, the program must support an **optional recursive copy** (using typical command-line syntax such as `-r` or `--recursive`).
    - If the recursive flag is used, it should copy directories and all their contents to `incoming_tmp`.
33. After the file or directory is copied into `incoming_tmp`, it must be **moved** to the corresponding location in `incoming`.
34. If a directory or file is copied, it must respect the profile and S3 bucket structure, meaning files are placed in the correct subdirectory based on the profile and bucket specified in the S3 URI.

#### Directory Handling:
35. An `incoming_tmp` directory with the same subdirectory structure as `incoming` must be created, where files are initially copied.
36. All new files must be first copied into the appropriate subdirectory of `incoming_tmp` for each profile and bucket.
37. Once a file copy is complete in `incoming_tmp`, the file should be moved to the corresponding location in the `incoming` directory, which is monitored by `fsnotify`.
38. When the program starts, it must first delete the **entire contents** of the `incoming_tmp` directory to ensure it is empty before continuing.
39. After deleting the contents of `incoming_tmp`, the program must proceed with creating all required directories for profiles and buckets under `incoming_tmp`, `incoming`, `processing`, `failed`, and `completed`.

#### File Processing:
40. Files are processed by **copying them to the target S3 profile** using the **Golang AWS SDK**.
41. The program must ensure that the file is fully uploaded to the S3 bucket before moving it to the `completed` directory.
   - The check should be **informational only** if the target S3 service does not support `HEAD` requests.
42. If the S3 upload fails, the program must log the error and move the file to the `failed` directory for retry or manual intervention.

#### Bucket Validation Against S3 Server:
43. Before copying any files to a bucket, the program must **validate the existence of the bucket on the S3 server**.
   - The program must:
     1. Query the S3 server for a list of buckets using the AWS SDK's `ListBuckets` function.
     2. Ensure that the bucket specified in the S3 URI (or derived from the directory structure) exists on the server

.
44. If the bucket does not exist on the S3 server, the program should:
   - Log an error indicating that the bucket does not exist.
   - Skip the operation for that specific bucket.

#### SQLite Database Logging:
45. The program must create an SQLite database in the current directory to track the state and progress of each file.
46. The SQLite database must contain a table named `file_records` with the following fields:
   - `id`: A unique identifier for each record (auto-incrementing primary key).
   - `profile`: The profile name.
   - `bucket`: The S3 bucket name.
   - `filepath`: The relative file path within the bucket.
   - `file_creation_date`: The file creation timestamp (to ensure a unique combination).
   - `current_state`: The current state of the file (e.g., `incoming`, `processing`, `completed`, `failed`).
   - `last_updated`: The timestamp when the state was last updated.
   - `upload_outcome`: The outcome of the upload (`success`, `failure`).
47. Each file should have a **unique record** in the database, determined by the `profile`, `bucket`, `filepath`, and **file creation date**.
48. If a **file enters the system again** with the same `profile`, `bucket`, `filepath`, and creation date (indicating a re-upload or retry), the program must create a **new record** in the database.
49. The program must update the database record **as the file moves through each state** (`incoming_tmp`, `incoming`, `processing`, `completed`, `failed`) and log the **outcome** (success or failure).

#### Credentials Search:
50. The program must search for the AWS credentials file using the **same algorithm** as the AWS CLI, before falling back to the file provided as an argument.

#### Logging and Output:
51. The program must output **detailed logging** and **information** about each action it performs, including file movements, copying, and S3 uploads.

---

### Summary of Key Changes:

- **Exponential backoff** retry mechanism for transient S3 errors, starting at 30 seconds, doubling on each attempt, up to 10 retries.
- **Maximum retry count** of 10 retries, after which the file is marked as failed.
- **SQLite logging** to track retry attempts and file states.
- **Abort execution** if the provider or required configuration keys are missing for any profile.

Let me know if you'd like any further adjustments or clarification!
