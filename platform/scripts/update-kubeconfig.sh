#!/bin/bash
set -e

# Configuration
CONTEXT_NAME="teamchikynbitts"
NEW_CONFIG="kubeconfig.new.yaml"
MERGED_CONFIG="kubeconfig.merged.yaml"
BACKUP_CONFIG="$HOME/.kube/config.bak.$(date +%s)"

# Ensure we are in the platform directory (where Pulumi.yaml lives)
if [ ! -f "Pulumi.yaml" ]; then
    echo "Error: This script must be run from the 'platform/' directory."
    exit 1
fi

echo "Fetching kubeconfig from Pulumi stack..."
pulumi stack output kubeconfig --show-secrets > "$NEW_CONFIG"

# Verify content
if [ ! -s "$NEW_CONFIG" ]; then
    echo "Error: Retrieved kubeconfig is empty or failed to download."
    rm -f "$NEW_CONFIG"
    exit 1
fi

echo "Renaming context/cluster/user to '$CONTEXT_NAME'..."
# Rename 'default' to 'teamchikynbitts' to avoid collisions with other clusters
# Using -i.bak for compatibility with both BSD (macOS) and GNU (Linux) sed
sed -i.bak "s/default/$CONTEXT_NAME/g" "$NEW_CONFIG"
rm -f "$NEW_CONFIG.bak"

# Check if ~/.kube/config exists
if [ -f "$HOME/.kube/config" ]; then
    echo "Backing up existing kubeconfig to $BACKUP_CONFIG..."
    cp "$HOME/.kube/config" "$BACKUP_CONFIG"
    
    echo "Merging with existing kubeconfig..."
    # KUBECONFIG merge order: first file takes precedence for overlaps, 
    # but we renamed ours so it should simply append/merge.
    # We put NEW_CONFIG last so if there is a weird collision, existing config is preferred? 
    # Actually, usually users want the NEW one to take effect if names match.
    # But names shouldn't match now.
    KUBECONFIG="$HOME/.kube/config:$NEW_CONFIG" kubectl config view --flatten > "$MERGED_CONFIG"
    
    mv "$MERGED_CONFIG" "$HOME/.kube/config"
else
    echo "No existing kubeconfig found at ~/.kube/config. Creating new one..."
    mkdir -p "$HOME/.kube"
    mv "$NEW_CONFIG" "$HOME/.kube/config"
fi

# Set permissions
chmod 600 "$HOME/.kube/config"

# Cleanup
rm -f "$NEW_CONFIG"

echo "Success! Context '$CONTEXT_NAME' has been added to your local ~/.kube/config."
echo "You can switch to it using:"
echo "  kubectl config use-context $CONTEXT_NAME"
echo "  # or if you use kubectx:"
echo "  kubectx $CONTEXT_NAME"
