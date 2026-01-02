#!/bin/bash
# Wrapper to run git with agent identity
set -e

REPO_ROOT=$(git rev-parse --show-toplevel)
CONFIG_FILE="$REPO_ROOT/agent-config.json"

if [ ! -f "$CONFIG_FILE" ]; then
  echo "Error: $CONFIG_FILE not found at repository root."
  exit 1
fi

# Simple JSON parsing using python3 which is standard on macOS
NAME=$(python3 -c "import sys, json; print(json.load(open('$CONFIG_FILE'))['git_profile']['name'])")
EMAIL=$(python3 -c "import sys, json; print(json.load(open('$CONFIG_FILE'))['git_profile']['email'])")
TOKEN=$(python3 -c "import sys, json; print(json.load(open('$CONFIG_FILE')).get('github_token', ''))")

export GIT_AUTHOR_NAME="$NAME"
export GIT_AUTHOR_EMAIL="$EMAIL"
export GIT_COMMITTER_NAME="$NAME"
export GIT_COMMITTER_EMAIL="$EMAIL"

# Handle push with token authentication
if [[ "$1" == "push" ]] && [[ -n "$TOKEN" ]]; then
    REMOTE_URL=$(git remote get-url origin)
    # Remove any existing auth info or protocol from URL for clean insertion
    # Assuming standard https://github.com/user/repo.git format
    CLEAN_URL=${REMOTE_URL#*https://}
    CLEAN_URL=${CLEAN_URL#*@} # Remove user@ if present
    
    # Construct authenticated URL
    AUTH_URL="https://$TOKEN@$CLEAN_URL"
    
    echo "Pushing as $NAME <$EMAIL>..."
    git push "$AUTH_URL" "${@:2}"
else
    git "$@"
fi
