package main

import (
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	ec2x "github.com/pulumi/pulumi-awsx/sdk/v2/go/awsx/ec2"
	"github.com/pulumi/pulumi-command/sdk/go/command/remote"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/yaml"
	"github.com/pulumi/pulumi-tls/sdk/v4/go/tls"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// 1. SSH Key Generation
		sshKey, err := tls.NewPrivateKey(ctx, "k3s-ssh-key", &tls.PrivateKeyArgs{
			Algorithm: pulumi.String("RSA"),
			RsaBits:   pulumi.Int(4096),
		})
		if err != nil {
			return err
		}

		keyPair, err := ec2.NewKeyPair(ctx, "k3s-keypair", &ec2.KeyPairArgs{
			PublicKey: sshKey.PublicKeyOpenssh,
		})
		if err != nil {
			return err
		}

		// 2. Network: Create a simple VPC
		vpc, err := ec2x.NewVpc(ctx, "eks-vpc", &ec2x.VpcArgs{
			AvailabilityZoneNames: []string{"us-east-1a", "us-east-1b"},
		})
		if err != nil {
			return err
		}

		// 3. Security Group
		sg, err := ec2.NewSecurityGroup(ctx, "k3s-sg", &ec2.SecurityGroupArgs{
			VpcId: vpc.VpcId,
			Ingress: ec2.SecurityGroupIngressArray{
				&ec2.SecurityGroupIngressArgs{
					Protocol:    pulumi.String("tcp"),
					FromPort:    pulumi.Int(22),
					ToPort:      pulumi.Int(22),
					CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					Description: pulumi.String("SSH"),
				},
				&ec2.SecurityGroupIngressArgs{
					Protocol:    pulumi.String("tcp"),
					FromPort:    pulumi.Int(6443),
					ToPort:      pulumi.Int(6443),
					CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					Description: pulumi.String("Kubernetes API"),
				},
				// Allow HTTP/HTTPS for Apps (via K3s Traefik/ServiceLB)
				&ec2.SecurityGroupIngressArgs{
					Protocol:    pulumi.String("tcp"),
					FromPort:    pulumi.Int(80),
					ToPort:      pulumi.Int(80),
					CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					Description: pulumi.String("HTTP"),
				},
				&ec2.SecurityGroupIngressArgs{
					Protocol:    pulumi.String("tcp"),
					FromPort:    pulumi.Int(443),
					ToPort:      pulumi.Int(443),
					CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					Description: pulumi.String("HTTPS"),
				},
			},
			Egress: ec2.SecurityGroupEgressArray{
				&ec2.SecurityGroupEgressArgs{
					Protocol:   pulumi.String("-1"),
					FromPort:   pulumi.Int(0),
					ToPort:     pulumi.Int(0),
					CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				},
			},
		})
		if err != nil {
			return err
		}

		// 4. AMI: Ubuntu 24.04 LTS
		ubuntu, err := ec2.LookupAmi(ctx, &ec2.LookupAmiArgs{
			MostRecent: pulumi.BoolRef(true),
			Owners:     []string{"099720109477"},
			Filters: []ec2.GetAmiFilter{
				{
					Name:   "name",
					Values: []string{"ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"},
				},
			},
		})
		if err != nil {
			return err
		}

		// 5. EC2 Instance
		// Install K3s via UserData
		userData := `#!/bin/bash
TOKEN=$(curl -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
PUBLIC_IP=$(curl -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/public-ipv4)
curl -sfL https://get.k3s.io | sh -s - --write-kubeconfig-mode 644 --tls-san $PUBLIC_IP
`
		instance, err := ec2.NewInstance(ctx, "k3s-server-v3", &ec2.InstanceArgs{
			Ami:                      pulumi.String(ubuntu.Id),
			InstanceType:             pulumi.String("t3.small"),
			VpcSecurityGroupIds:      pulumi.StringArray{sg.ID()},
			SubnetId:                 vpc.PublicSubnetIds.Index(pulumi.Int(0)),
			AssociatePublicIpAddress: pulumi.Bool(true),
			KeyName:                  keyPair.KeyName,
			UserData:                 pulumi.String(userData),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("k3s-server-v3"),
			},
		})
		if err != nil {
			return err
		}

		ctx.Export("publicIP", instance.PublicIp)
		ctx.Export("privateKey", sshKey.PrivateKeyOpenssh)

		// 6. Retrieve Kubeconfig
		// We use a remote command to CAT the file.
		// We depend on the instance enabling SSH, which takes a moment.
		// The Connection uses the Public IP.
		kubeconfigCmd, err := remote.NewCommand(ctx, "get-kubeconfig", &remote.CommandArgs{
			Connection: &remote.ConnectionArgs{
				Host:       instance.PublicIp,
				User:       pulumi.String("ubuntu"),
				PrivateKey: sshKey.PrivateKeyOpenssh,
			},
			Create: pulumi.String("for i in {1..20}; do if [ -f /etc/rancher/k3s/k3s.yaml ]; then cat /etc/rancher/k3s/k3s.yaml; exit 0; fi; sleep 5; done; echo 'Timed out waiting for kubeconfig'; exit 1"),
		}, pulumi.DependsOn([]pulumi.Resource{instance}))
		if err != nil {
			return err
		}

		// Fix the Kubeconfig: Replace 127.0.0.1 with Public IP
		kubeconfig := pulumi.All(kubeconfigCmd.Stdout, instance.PublicIp).ApplyT(
			func(args []interface{}) (string, error) {
				kconf := args[0].(string)
				ip := args[1].(string)
				return strings.Replace(kconf, "127.0.0.1", ip, -1), nil
			}).(pulumi.StringOutput)

		ctx.Export("kubeconfig", kubeconfig)

		// 7. Kubernetes Provider
		k8sProvider, err := kubernetes.NewProvider(ctx, "k3s-provider", &kubernetes.ProviderArgs{
			Kubeconfig: kubeconfig,
		})
		if err != nil {
			return err
		}

		// 8. Install Flux V2 via Helm
		fluxRelease, err := helm.NewRelease(ctx, "flux2", &helm.ReleaseArgs{
			Chart:   pulumi.String("flux2"),
			Version: pulumi.String("2.13.0"), // Stable version
			RepositoryOpts: &helm.RepositoryOptsArgs{
				Repo: pulumi.String("https://fluxcd-community.github.io/helm-charts"),
			},
			Namespace:       pulumi.String("flux-system"),
			CreateNamespace: pulumi.Bool(true),
		}, pulumi.Provider(k8sProvider))
		if err != nil {
			return err
		}

		// 9. Flux GitRepository
		// The Source for our Apps
		repoYAML := `apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: teamchikynbitts-repo
  namespace: flux-system
spec:
  interval: 1m0s
  url: https://github.com/joshuamdhayes/teamchikynbitts
  ref:
    branch: main
`
		// We need to wait for Flux CRDs to be installed by the Helm chart
		gitRepo, err := yaml.NewConfigGroup(ctx, "flux-repo", &yaml.ConfigGroupArgs{
			YAML: []string{repoYAML},
		}, pulumi.Provider(k8sProvider), pulumi.DependsOn([]pulumi.Resource{fluxRelease}))
		if err != nil {
			return err
		}

		// 10. Flux Kustomizations
		// App 1: TeamChikynbitts App
		app1YAML := `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: teamchikynbitts-app
  namespace: flux-system
spec:
  interval: 1m0s
  targetNamespace: default
  sourceRef:
    kind: GitRepository
    name: teamchikynbitts-repo
  path: "./app/teamchikynbitts-app/k8s"
  prune: true
  wait: true
`

		// App 2: Josh App
		app2YAML := `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: josh-app
  namespace: flux-system
spec:
  interval: 1m0s
  targetNamespace: default
  sourceRef:
    kind: GitRepository
    name: teamchikynbitts-repo
  path: "./app/josh-app/k8s"
  prune: true
  wait: true
`
		_, err = yaml.NewConfigGroup(ctx, "flux-apps", &yaml.ConfigGroupArgs{
			YAML: []string{app1YAML, app2YAML},
		}, pulumi.Provider(k8sProvider), pulumi.DependsOn([]pulumi.Resource{gitRepo}))
		if err != nil {
			return err
		}

		return nil
	})
}
