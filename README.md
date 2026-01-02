# Team Chikynbitts Infrastructure

This repository manages the AWS infrastructure and Kubernetes/GitOps environment for Team Chikynbitts. It is designed as a learning platform for Kubernetes (**K3s**) and **Flux** GitOps deployments, structured using Platform Engineering best practices.

## Project Structure

The project is organized into three distinct layers, each representing a separation of concerns:

### 1. `foundation/` (Cloud Foundation)
**Owner:** Cloud Administrators
**Purpose:** Bootstraps the AWS account with necessary Identity and Cost controls.
-   **IAM Users:** Manages access for team members (Joshua, Justin, Abby).
-   **Budgets:** Enforces strict cost alerts ($50 warning, $75 critical) to keep the demo account cheap.
-   **Security:** Enforces MFA policies for all administrators.
-   **ECR:** Private container registry for storing application images.

### 2. `platform/` (Kubernetes Platform)
**Owner:** Platform Engineers
**Purpose:** Provisions the "Lightweight" Kubernetes cluster.
-   **K3s**: Self-managed K3s cluster on a single `t3.small` EC2 instance (Ubuntu 24.04).
-   **Flux:** GitOps controller for continuous delivery (replaced ArgoCD).
-   **Networking**: Custom VPC configuration with Public IP access.

### 3. `app/` (Application)
**Owner:** Developers
**Purpose:** The source code and manifests for the business application.
-   **Src:** A simple Go web server.
-   **K8s:** Kubernetes Deployment, Service (ClusterIP), and Ingress manifests.
-   **GitOps:** Flux syncs this directory to the cluster.

---

## Getting Started

### Prerequisites
-   [Pulumi CLI](https://www.pulumi.com/docs/install/)
-   [Go 1.24+](https://go.dev/dl/)
-   AWS Credentials configured

### 1. Setup Foundation & Users
The foundation layer requires a list of users to generate IAM access. **This file is gitignored for security.**

1.  Navigate to `foundation/`:
    ```bash
    cd foundation
    ```
2.  Create a `users.json` file:
    ```json
    [
      "Joshua Hayes",
      "Justin Rouse",
      "Abby Adkins"
    ]
    ```
3.  Deploy the foundation:
    ```bash
    pulumi up
    ```
    *Output will provide the initial Access Keys and Secrets for each user, and the ECR Repository URL.*

    #### Using ECR (Registry)
    To push images to the shared registry:
    1.  **Login**: `aws ecr get-login-password | docker login --username AWS --password-stdin <RepositoryURL>`
    2.  **Build**: `docker build -t <RepositoryURL>:v1 .`
    3.  **Push**: `docker push <RepositoryURL>:v1`

### 2. Deploy the Platform
When ready to work (and incur costs), spin up the platform.

1.  Navigate to `platform/`:
    ```bash
    cd ../platform
    ```
2.  Deploy the cluster:
    ```bash
    pulumi up
    ```
    *This takes ~2-5 minutes.*

### 3. Access & Verify
-   **Kubeconfig**: Pulumi will export the kubeconfig. Save it to access the cluster:
    ```bash
    pulumi stack output kubeconfig --show-secrets > kubeconfig.yaml
    export KUBECONFIG=$PWD/kubeconfig.yaml
    kubectl get nodes
    ```
-   **SSH Access**: Retrieve your private key if needed for debugging:
    ```bash
    pulumi stack output privateKey --show-secrets > key.pem
    chmod 600 key.pem
    ssh -i key.pem ubuntu@$(pulumi stack output publicIP)
    ```
-   **Flux**: Check the status of GitOps sync:
    ```bash
    kubectl get kustomizations -n flux-system
    ```

### 4. Accessing Applications
The cluster uses `Traefik` Ingress with Host-based routing. To access the applications, you must use the instance's **Public IP** with the correct **Host Header**.

1.  Get the Public IP:
    ```bash
    export PUBLIC_IP=$(pulumi stack output publicIP)
    echo $PUBLIC_IP
    ```

2.  **Access via Curl**:
    ```bash
    # Josh App
    curl -H "Host: josh-app.local" http://$PUBLIC_IP
    
    # Team App
    curl -H "Host: team-app.local" http://$PUBLIC_IP
    ```

3.  **Access via Browser**:
    Add the IP to your `/etc/hosts` file:
    ```
    <PUBLIC_IP> josh-app.local team-app.local
    ```
    Then visit `http://josh-app.local` or `http://team-app.local` in your browser.

---

## Tear Down & Cost Savings

**IMPORTANT:** Always tear down the platform when finished to stop the EC2 charges.

```bash
cd platform
pulumi destroy
```
Since K3s uses a simplified LoadBalancer (ServiceLB) on the node itself, there are no AWS ELBs to clean up manually. `pulumi destroy` will terminate the EC2 instance and stop all costs.
