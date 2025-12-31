# Application Directory

This directory contains the source code and manifests for all applications deployed to the cluster.

## Structure
To support multiple applications and developers, each application should have its own directory:

-   `teamchikynbitts-app/`: The core team demo application.
-   `[app-name]/`: Your new application.

## Creating a New App
1.  Create a new directory: `mkdir my-new-app`
2.  Add your source code and `Dockerfile`.
3.  Add Kubernetes manifests in a `k8s/` subdirectory.
4.  Configure ArgoCD to deploy it (requires Platform updates).
