#!/bin/bash
# Get the cluster's public IP and display app URLs
# Works without Pulumi login - uses kubectl or AWS CLI
# Dynamically discovers apps from the app/ directory

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
APP_DIR="$REPO_ROOT/app"

# Function to get public IP
get_public_ip() {
    # Try kubectl first (preferred, no AWS auth needed)
    if command -v kubectl &> /dev/null; then
        IP=$(kubectl get configmap cluster-vars -n flux-system -o jsonpath='{.data.PUBLIC_IP}' 2>/dev/null)
        if [ -n "$IP" ]; then
            echo "$IP"
            return 0
        fi
        
        # Fallback: get from node external IP
        IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="ExternalIP")].address}' 2>/dev/null)
        if [ -n "$IP" ]; then
            echo "$IP"
            return 0
        fi
    fi

    # Fallback: try AWS CLI
    if command -v aws &> /dev/null; then
        IP=$(aws ec2 describe-addresses --filters "Name=tag:Name,Values=k3s-eip" --query "Addresses[0].PublicIp" --output text 2>/dev/null)
        if [ -n "$IP" ] && [ "$IP" != "None" ]; then
            echo "$IP"
            return 0
        fi
    fi

    return 1
}

# Get public IP
IP=$(get_public_ip)
if [ -z "$IP" ]; then
    echo "Could not determine public IP."
    echo "Make sure kubectl is configured or AWS CLI has access."
    exit 1
fi

echo "Public IP: $IP"
echo ""
echo "App URLs:"

# Scan app/ directory for Ingress manifests and extract hostnames
for app_dir in "$APP_DIR"/*/; do
    app_name=$(basename "$app_dir")
    
    # Look for manifests in k8s/ subdirectory
    for manifest in "$app_dir"k8s/*.yaml "$app_dir"k8s/*.yml; do
        [ -f "$manifest" ] || continue
        
        # Extract host entries from Ingress resources, substitute ${PUBLIC_IP}
        hosts=$(grep -E '^\s*-?\s*host:' "$manifest" 2>/dev/null | sed 's/.*host:\s*//' | sed "s/\${PUBLIC_IP}/$IP/g" | tr -d '"' | tr -d "'")
        
        for host in $hosts; do
            # Skip if it's just a variable reference that wasn't substituted
            [[ "$host" == *'${'* ]] && continue
            echo "  $app_name: http://$host"
        done
    done
done
