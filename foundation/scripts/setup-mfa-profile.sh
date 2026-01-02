#!/bin/bash
set -e

# Configuration
PROFILE_NAME="teamchikynbitts"
REGION="us-east-1"

echo "This script will configure an AWS CLI profile with automatic MFA usage."
echo "You will no longer need to run 'aws sts get-session-token' manually."
echo ""

# verify aws cli is installed
if ! command -v aws &> /dev/null; then
    echo "Error: AWS CLI is not installed."
    exit 1
fi

# Prompt for Long-term Credentials
read -p "Enter your Access Key ID: " ACCESS_KEY
read -s -p "Enter your Secret Access Key: " SECRET_KEY
echo ""
read -p "Enter your MFA Device ARN (e.g., arn:aws:iam::123:mfa/user): " MFA_SERIAL

echo ""
echo "Configuring profile '$PROFILE_NAME'..."

# Set credentials for the profile
aws configure set aws_access_key_id "$ACCESS_KEY" --profile "$PROFILE_NAME"
aws configure set aws_secret_access_key "$SECRET_KEY" --profile "$PROFILE_NAME"
aws configure set region "$REGION" --profile "$PROFILE_NAME"
aws configure set output "json" --profile "$PROFILE_NAME"

# Set MFA Serial
aws configure set mfa_serial "$MFA_SERIAL" --profile "$PROFILE_NAME"

echo ""
echo "Success! Profile '$PROFILE_NAME' configured."
echo ""
echo "How to use it:"
echo "1. Run: export AWS_PROFILE=$PROFILE_NAME"
echo "2. Run any command: aws s3 ls"
echo "3. The AWS CLI will automatically ask 'Enter MFA code for ...'"
echo "4. It will cache the session token for you!"
echo "Alternatively you can add the following to your ~/.zshrc or ~/.bashrc:"
echo "export AWS_PROFILE=$PROFILE_NAME"
