#!/bin/bash
set -e

# Ensure we are in the foundation directory
if [ ! -f "Pulumi.yaml" ]; then
    echo "Error: This script must be run from the 'foundation/' directory."
    exit 1
fi

ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
LOGIN_URL="https://${ACCOUNT_ID}.signin.aws.amazon.com/console"

echo "=================================================================="
echo " AWS CONSOLE CREDENTIALS"
echo " Login URL: $LOGIN_URL"
echo "=================================================================="

# Get all outputs in JSON format
OUTPUTS=$(pulumi stack output --show-secrets -j)

# Read users.json
if [ ! -f "users.json" ]; then
    echo "Error: users.json not found."
    exit 1
fi

# Parse users and print credentials
jq -r '.[].name' users.json | while read -r FULL_NAME; do
    # Sanitize name to match resource name logic (lowercase, spaces to dashes)
    RESOURCE_NAME=$(echo "$FULL_NAME" | tr '[:upper:]' '[:lower:]' | tr ' ' '-')
    
    PASSWORD=$(echo "$OUTPUTS" | jq -r --arg key "ConsolePassword-$RESOURCE_NAME" '.[$key] // empty')

    if [ -n "$PASSWORD" ]; then
        echo ""
        echo "------------------------------------------------------------------"
        echo " User: $FULL_NAME"
        echo " Username: $RESOURCE_NAME"
        echo " Password: $PASSWORD"
        echo "------------------------------------------------------------------"
        echo "Instructions:"
        echo "1. Go to $LOGIN_URL"
        echo "2. Log in with the username and password above."
        echo "3. You will be prompted to change your password immediately."
    else
        echo "Warning: No password found for user '$FULL_NAME' (Resource: $RESOURCE_NAME)"
    fi
done
echo ""
