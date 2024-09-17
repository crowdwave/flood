To access **Cloudflare R2** using the official AWS S3 tools (such as `aws-cli` or the AWS SDKs), you need to configure the credentials and specify the endpoint to direct the S3 API requests to Cloudflare's R2 storage instead of AWS. This is done by adjusting the credentials and config files, as well as setting the proper S3 endpoint for R2.

### Steps to Access Cloudflare R2 with AWS S3 Tools:

1. **Set Up Cloudflare R2 Credentials**:
   Cloudflare R2 uses an S3-compatible API, so you’ll need to use the `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` that you generated for your R2 bucket.

2. **Modify the AWS Credentials File** (`~/.aws/credentials`):
   Add your Cloudflare R2 credentials to the AWS credentials file.

   ```ini
   [cloudflare-r2]
   aws_access_key_id = <your-access-key-id>
   aws_secret_access_key = <your-secret-access-key>
   ```

3. **Configure the AWS Config File** (`~/.aws/config`):
   Add the custom endpoint for Cloudflare R2 to the AWS config file.

   ```ini
   [profile cloudflare-r2]
   region = auto
   output = json
   s3 =
       endpoint_url = https://<your-r2-account-id>.r2.cloudflarestorage.com
       signature_version = s3v4
   ```

   - **`<your-access-key-id>`**: The access key ID you generated in Cloudflare for R2.
   - **`<your-secret-access-key>`**: The secret access key for your Cloudflare R2 account.
   - **`<your-r2-account-id>`**: This is your Cloudflare account ID, which is part of the R2 endpoint URL.

4. **Using the AWS CLI**:
   After configuring the credentials and endpoint, you can use AWS CLI commands to interact with your Cloudflare R2 bucket just like you would with an AWS S3 bucket.

   Example command to list buckets:
   ```bash
   aws s3 ls --profile cloudflare-r2
   ```

5. **Accessing Specific Buckets**:
   To interact with a specific bucket, include the bucket name in your S3 command.

   Example command to list files in a bucket:
   ```bash
   aws s3 ls s3://your-bucket-name --profile cloudflare-r2
   ```

### Explanation:
- **`aws_access_key_id` / `aws_secret_access_key`**: These are the credentials created for your Cloudflare R2 account.
- **`endpoint_url`**: This points to Cloudflare’s R2 endpoint instead of AWS S3. The format is typically `https://<account-id>.r2.cloudflarestorage.com`.
- **`signature_version = s3v4`**: Cloudflare R2 supports AWS's Signature Version 4, which is required for authentication.

### Additional Notes:
- You can also set these credentials and configurations as environment variables if you prefer not to use the AWS credentials and config files.
- For example:
   ```bash
   export AWS_ACCESS_KEY_ID=<your-access-key-id>
   export AWS_SECRET_ACCESS_KEY=<your-secret-access-key>
   export AWS_DEFAULT_REGION=auto
   ```

By following this setup, you can seamlessly use AWS S3 tools with Cloudflare R2 as the backend storage provider.
