package main

import (
	"encoding/json"

	"github.com/pulumi/pulumi-awsx/sdk/v2/go/awsx/ec2"
	"github.com/pulumi/pulumi-eks/sdk/v3/go/eks"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/yaml"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Create a VPC configuration to restrict AZs and avoid us-east-1e
		vpc, err := ec2.NewVpc(ctx, "eks-vpc", &ec2.VpcArgs{
			AvailabilityZoneNames: []string{
				"us-east-1a",
				"us-east-1b",
			},
		})
		if err != nil {
			return err
		}

		// Create an EKS cluster with the default configuration.
		// We use t3.small instances to keep costs low.
		cluster, err := eks.NewCluster(ctx, "cluster", &eks.ClusterArgs{
			VpcId:            vpc.VpcId,
			PublicSubnetIds:  vpc.PublicSubnetIds,
			PrivateSubnetIds: vpc.PrivateSubnetIds,
			InstanceType:     pulumi.String("t3.small"),
			DesiredCapacity:  pulumi.Int(2),
			MinSize:          pulumi.Int(1),
			MaxSize:          pulumi.Int(2),
		})
		if err != nil {
			return err
		}

		// Export the cluster's kubeconfig.
		ctx.Export("kubeconfig", cluster.Kubeconfig)

		// Convert AnyOutput kubeconfig to StringOutput for the Provider
		kubeconfigString := cluster.Kubeconfig.ApplyT(func(v interface{}) (string, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return string(b), nil
		}).(pulumi.StringOutput)

		// Create a Kubernetes provider instance that uses our cluster from above.
		k8sProvider, err := kubernetes.NewProvider(ctx, "k8s-provider", &kubernetes.ProviderArgs{
			Kubeconfig: kubeconfigString,
		})
		if err != nil {
			return err
		}

		// Install ArgoCD using the Helm chart.
		argocdRelease, err := helm.NewRelease(ctx, "argocd", &helm.ReleaseArgs{
			Chart:   pulumi.String("argo-cd"),
			Version: pulumi.String("7.7.11"), // Use a specific stable version
			RepositoryOpts: &helm.RepositoryOptsArgs{
				Repo: pulumi.String("https://argoproj.github.io/argo-helm"),
			},
			Namespace:       pulumi.String("argocd"),
			CreateNamespace: pulumi.Bool(true),
			Values: pulumi.Map{
				"server": pulumi.Map{
					"service": pulumi.Map{
						"type": pulumi.String("LoadBalancer"),
					},
				},
			},
		}, pulumi.Provider(k8sProvider))
		if err != nil {
			return err
		}

		// Define the ArgoCD Application to point to the repository using a YAML manifest.
		appYAML := `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: teamchikynbitts-app
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/joshuamdhayes/teamchikynbitts
    targetRevision: HEAD
    path: app/k8s
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
`

		_, err = yaml.NewConfigGroup(ctx, "teamchikynbitts-app", &yaml.ConfigGroupArgs{
			YAML: []string{appYAML},
		}, pulumi.Provider(k8sProvider), pulumi.DependsOn([]pulumi.Resource{argocdRelease}))
		if err != nil {
			return err
		}

		return nil
	})
}
