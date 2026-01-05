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

## Architectural Decisions

This environment makes several trade-offs to prioritize **cost-optimization** and **resource efficiency** for a single-node setup:

*   **K3s vs EKS**: We use K3s on a single EC2 instance to avoid the ~$72/month EKS control plane fee. K3s is a highly efficient, production-ready distribution perfect for small clusters.
*   **Flux vs ArgoCD**: Flux was chosen for its lower memory footprint compared to ArgoCD, which is critical on a `t3.small` (2GB RAM) node.
*   **Elastic IP vs ALB**: We use an AWS Elastic IP (EIP) combined with `nip.io` magic DNS. This provides a stable public URL for apps while avoiding the ~$18/month cost of an AWS Application Load Balancer.
*   **Default Ingress (Traefik)**: K3s includes Traefik by default, which we use to handle host-based routing across multiple applications on a single public IP.

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
      {
        "name": "Joshua Hayes",
        "groups": ["technical", "billing"]
      },
      {
        "name": "Justin Rouse",
        "groups": ["technical"]
      },
      {
        "name": "Abby Adkins",
        "groups": ["technical"]
      }
    ]
    ```
3.  Deploy the foundation:
    ```bash
    pulumi up
    ```
    *Output will provide the initial Access Keys and Secrets for each user, and the ECR Repository URL.*

4.  **Onboarding Users & MFA**:
    After deployment, you can retrieve temporary console passwords for yourself and your team:
    ```bash
    ./scripts/get-creds.sh
    ```
    Each user should then:
    1.  Log in to the [AWS Console](https://signin.aws.amazon.com/console).
    2.  Update their password when prompted.
    3.  **Immediately** set up MFA (Multi-Factor Authentication) under "Security Credentials". We recommend using a Virtual MFA device (authenticator app) or a hardware key.

5.  **AWS CLI MFA Setup**:
    Since our AWS account enforces MFA for all actions, you need to generate a temporary session token to use the CLI. We provide a helper script to make this easy.

    **Prerequisite**: Ensure you have saved your **MFA Device ARN** (found in IAM -> Security Credentials). It looks like `arn:aws:iam::123456789:mfa/DeviceName`.

    **To Login:**
    1.  Run the login script:
        ```bash
        ./foundation/scripts/aws-login.sh <YOUR_6_DIGIT_CODE>
        ```
        *(First time run will ask for your MFA Device ARN and cache it).*
    2.  Copy and Paste the `export` commands output by the script into your terminal.
    3.  Verify access:
        ```bash
        aws s3 ls
        ```
    *Note: The session token is valid for 12 hours. You will need to repeat this process when it expires.*

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
-   **Kubeconfig**: We have a script to automatically merge the cluster config into your local `~/.kube/config`:
    ```bash
    ./scripts/update-kubeconfig.sh
    # Switch context
    kubectl config use-context teamchikynbitts
    # Or use kubectx
    kubectx teamchikynbitts
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
The cluster uses `Traefik` Ingress with `nip.io` domains (magic DNS) to route traffic. You can access the apps directly in your browser:

*   **Josh App**: `http://josh-app.34.235.5.146.nip.io`
*   **Team App**: `http://team-app.34.235.5.146.nip.io`

*Note: If the EC2 instance is replaced, the IP will change, and these manifests/URLs will need to be updated.*

---

## Tear Down & Cost Savings

**IMPORTANT:** Always tear down the platform when finished to stop the EC2 charges.

```bash
cd platform
pulumi destroy
```
Since K3s uses a simplified LoadBalancer (ServiceLB) on the node itself, there are no AWS ELBs to clean up manually. `pulumi destroy` will terminate the EC2 instance and stop all costs.
