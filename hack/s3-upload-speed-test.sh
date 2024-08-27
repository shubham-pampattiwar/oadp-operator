#!/bin/bash

# Variables
BUCKET_NAME="<BUCKET_NAME>"
REGION="us-east-1"
FILE_SIZE_MB=100        # Size of the file in MB
TEST_FILE="testfile"
S3_PATH="s3://$BUCKET_NAME/test-upload-speed"
AWS_CLI_PROFILE="default"   

# Generate a file with specified size
echo "Generating a $FILE_SIZE_MB MB test file..."
dd if=/dev/zero of=$TEST_FILE bs=1M count=$FILE_SIZE_MB

START_TIME=$(date +%s)

echo "Uploading file to S3..."
aws s3 cp $TEST_FILE $S3_PATH --region $REGION --profile $AWS_CLI_PROFILE

END_TIME=$(date +%s)

ELAPSED_TIME=$((END_TIME - START_TIME))

UPLOAD_SPEED=$(echo "$FILE_SIZE_MB / $ELAPSED_TIME" | bc -l)

echo "Upload completed in $ELAPSED_TIME seconds."
echo "Upload speed: $UPLOAD_SPEED MB/s"

rm -f $TEST_FILE
