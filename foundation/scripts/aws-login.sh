#!/bin/bash
set -e

# Configuration
PROFILE="${AWS_PROFILE:-teamchikynbitts}"

# 1. Get MFA_SERIAL
if [ -z "$MFA_SERIAL" ]; then
  # Try to read from a local cache file for convenience
  if [ -f "$HOME/.aws/mfa_serial_$PROFILE" ]; then
    MFA_SERIAL=$(cat "$HOME/.aws/mfa_serial_$PROFILE")
  fi
fi

if [ -z "$MFA_SERIAL" ]; then
  echo "Enter your MFA Device ARN (e.g., arn:aws:iam::123:mfa/user):"
  read -r MFA_SERIAL
  # Save for next time
  echo "$MFA_SERIAL" > "$HOME/.aws/mfa_serial_$PROFILE"
  echo "Saved MFA Serial to $HOME/.aws/mfa_serial_$PROFILE for future use."
fi

# 2. Get Token Code
if [ -n "$1" ]; then
    TOKEN_CODE=$1
else
    echo "Enter your 6-digit MFA Code:"
    read -r TOKEN_CODE
fi

echo "Authenticating via MFA ($PROFILE)..."
# We use the profile to get the session token
JSON=$(aws sts get-session-token --profile "$PROFILE" --serial-number "$MFA_SERIAL" --token-code "$TOKEN_CODE" --output json)

if [ $? -ne 0 ]; then
  echo "Failed to get session token. Check your MFA code and ARN."
  exit 1
fi

# Extract credentials
AWS_ACCESS_KEY_ID=$(echo "$JSON" | jq -r '.Credentials.AccessKeyId')
AWS_SECRET_ACCESS_KEY=$(echo "$JSON" | jq -r '.Credentials.SecretAccessKey')
AWS_SESSION_TOKEN=$(echo "$JSON" | jq -r '.Credentials.SessionToken')

# Export for the current shell session (printed to be evaluated)
echo ""
echo "###########################################################"
echo "  PASTE THE FOLLOWING INTO YOUR TERMINAL TO ACTIVATE:"
echo "###########################################################"
echo ""
echo "export AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID"
echo "export AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY"
echo "export AWS_SESSION_TOKEN=$AWS_SESSION_TOKEN"
echo ""
echo "###########################################################"
