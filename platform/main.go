package main

import (
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	ec2x "github.com/pulumi/pulumi-awsx/sdk/v2/go/awsx/ec2"
	"github.com/pulumi/pulumi-command/sdk/go/command/remote"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes"
	corev1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/core/v1"
	"github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/helm/v3"
	metav1 "github.com/pulumi/pulumi-kubernetes/sdk/v4/go/kubernetes/meta/v1"
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

		// 4a. IAM Role for ECR Access
		role, err := iam.NewRole(ctx, "k3s-role", &iam.RoleArgs{
			AssumeRolePolicy: pulumi.String(`{
				"Version": "2012-10-17",
				"Statement": [{
					"Action": "sts:AssumeRole",
					"Effect": "Allow",
					"Principal": {
						"Service": "ec2.amazonaws.com"
					}
				}]
			}`),
		})
		if err != nil {
			return err
		}

		_, err = iam.NewRolePolicyAttachment(ctx, "k3s-ecr-attach", &iam.RolePolicyAttachmentArgs{
			Role:      role.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"),
		})
		if err != nil {
			return err
		}

		// Also SSM for Session Manager (Debug access)
		_, err = iam.NewRolePolicyAttachment(ctx, "k3s-ssm-attach", &iam.RolePolicyAttachmentArgs{
			Role:      role.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"),
		})
		if err != nil {
			return err
		}

		instanceProfile, err := iam.NewInstanceProfile(ctx, "k3s-profile", &iam.InstanceProfileArgs{
			Role: role.Name,
		})
		if err != nil {
			return err
		}

		// 5. Elastic IP (Stable Public Access)
		// We use an EIP to ensure the public IP address is static across instance replacements.
		// This is a cost-effective alternative to an AWS Application Load Balancer (ALB).
		// While LB costs ~$18/mo, an EIP is $0 while attached to a running instance.
		eip, err := ec2.NewEip(ctx, "k3s-eip", &ec2.EipArgs{
			Vpc: pulumi.Bool(true),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("k3s-eip"),
			},
		})
		if err != nil {
			return err
		}

		ctx.Export("publicIP", eip.PublicIp)

		// 6. EC2 Instance
		// Install K3s via UserData (with IMDSv2 token)
		// We explicitly add the EIP to the TLS SAN list.
		userData := pulumi.Sprintf(`#!/bin/bash
TOKEN=$(curl -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
PUBLIC_IP=$(curl -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/public-ipv4)
curl -sfL https://get.k3s.io | sh -s - --write-kubeconfig-mode 644 --tls-san $PUBLIC_IP --tls-san %s
`, eip.PublicIp)

		instance, err := ec2.NewInstance(ctx, "k3s-server-v6", &ec2.InstanceArgs{
			Ami:                      pulumi.String(ubuntu.Id),
			InstanceType:             pulumi.String("t3.small"),
			VpcSecurityGroupIds:      pulumi.StringArray{sg.ID()},
			SubnetId:                 vpc.PublicSubnetIds.Index(pulumi.Int(0)),
			IamInstanceProfile:       instanceProfile.Name,
			AssociatePublicIpAddress: pulumi.Bool(true),
			KeyName:                  keyPair.KeyName,
			UserData:                 userData,
			Tags: pulumi.StringMap{
				"Name": pulumi.String("k3s-server-v6"),
			},
		})
		if err != nil {
			return err
		}

		// Associate the EIP with the new instance
		_, err = ec2.NewEipAssociation(ctx, "k3s-eip-assoc", &ec2.EipAssociationArgs{
			InstanceId:   instance.ID(),
			AllocationId: eip.AllocationId,
		})
		if err != nil {
			return err
		}

		ctx.Export("privateKey", sshKey.PrivateKeyOpenssh)

		// 7. Retrieve Kubeconfig
		// We use a remote command to CAT the file.
		// We depend on the instance enabling SSH, which takes a moment.
		// The Connection uses the Public IP.
		kubeconfigCmd, err := remote.NewCommand(ctx, "get-kubeconfig-v2", &remote.CommandArgs{
			Connection: &remote.ConnectionArgs{
				Host:       eip.PublicIp, // Use EIP for connection
				User:       pulumi.String("ubuntu"),
				PrivateKey: sshKey.PrivateKeyOpenssh,
			},
			Create: pulumi.String("for i in {1..20}; do if [ -f /etc/rancher/k3s/k3s.yaml ]; then cat /etc/rancher/k3s/k3s.yaml; exit 0; fi; sleep 5; done; echo 'Timed out waiting for kubeconfig'; exit 1"),
			Triggers: pulumi.Array{
				instance.ID(),
			},
		}, pulumi.DependsOn([]pulumi.Resource{instance}))
		if err != nil {
			return err
		}

		// Fix the Kubeconfig: Replace 127.0.0.1 with Public IP (EIP)
		kubeconfig := pulumi.All(kubeconfigCmd.Stdout, eip.PublicIp).ApplyT(
			func(args []interface{}) (string, error) {
				kconf := args[0].(string)
				ip := args[1].(string)
				return strings.Replace(kconf, "127.0.0.1", ip, -1), nil
			}).(pulumi.StringOutput)

		ctx.Export("kubeconfig", pulumi.ToSecret(kubeconfig))
		ctx.Export("publicIP", eip.PublicIp)
		ctx.Export("privateKey", sshKey.PrivateKeyOpenssh)

		// 7. Kubernetes Provider
		k8sProvider, err := kubernetes.NewProvider(ctx, "k3s-provider-v2", &kubernetes.ProviderArgs{
			Kubeconfig: kubeconfig,
		})
		if err != nil {
			return err
		}

		// 8. Install Flux V2 via Helm
		// Flux was chosen over ArgoCD to reduce resource consumption on the single t3.small node.
		// It manages GitOps synchronization by watching the repository for manifest changes.
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

		// 10. ECR Credentials CronJob
		// K3s needs a way to pull private images from ECR. Since ECR tokens expire every 12 hours,
		// we deploy a CronJob that refreshes the 'regcred' secret every 6 hours.
		ecrCronYAML := `apiVersion: v1
kind: ServiceAccount
metadata:
  name: ecr-refresher
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ecr-refresher
rules:
- apiGroups: [""]
  resources: ["secrets", "serviceaccounts"]
  verbs: ["get", "delete", "create", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ecr-refresher
subjects:
- kind: ServiceAccount
  name: ecr-refresher
  namespace: default
roleRef:
  kind: ClusterRole
  name: ecr-refresher
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: ecr-refresher
  namespace: default
spec:
  schedule: "0 */6 * * *" # Every 6 hours
  successfulJobsHistoryLimit: 1
  failedJobsHistoryLimit: 1
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: ecr-refresher
          containers:
          - name: refresher
            image: amazon/aws-cli:latest
            command:
            - /bin/bash
            - -c
            - |
              # Install kubectl
              curl -o kubectl https://s3.us-west-2.amazonaws.com/amazon-eks/1.27.1/2023-04-19/bin/linux/amd64/kubectl
              chmod +x kubectl
              mv kubectl /usr/bin/
              
              # Get ECR Token
              echo "Getting ECR Token..."
              TOKEN=$(aws ecr get-login-password --region us-east-1)
              REGISTRY="347788108263.dkr.ecr.us-east-1.amazonaws.com"
              
              # Delete existing secret (ignore if not exists)
              # Update secrets in all app namespaces
              NAMESPACES=("default" "josh-app" "teamchikynbitts-app")
              for NS in "${NAMESPACES[@]}"; do
                echo "Updating secret in namespace $NS"
                kubectl delete secret regcred -n $NS --ignore-not-found
                kubectl create secret docker-registry regcred -n $NS \
                  --docker-server=$REGISTRY \
                  --docker-username=AWS \
                  --docker-password=$TOKEN
                kubectl patch serviceaccount default -n $NS -p '{"imagePullSecrets":[{"name":"regcred"}]}' || echo "SA not found in $NS, skipping patch"
              done
              
              echo "Done!"
          restartPolicy: Never
---
apiVersion: batch/v1
kind: Job
metadata:
  name: ecr-refresher-init
  namespace: default
spec:
  template:
    spec:
      serviceAccountName: ecr-refresher
      containers:
      - name: refresher
        image: amazon/aws-cli:latest
        command:
        - /bin/bash
        - -c
        - |
          # Install kubectl
          curl -o kubectl https://s3.us-west-2.amazonaws.com/amazon-eks/1.27.1/2023-04-19/bin/linux/amd64/kubectl
          chmod +x kubectl
          mv kubectl /usr/bin/
          
          # Get ECR Token
          echo "Getting ECR Token..."
          TOKEN=$(aws ecr get-login-password --region us-east-1)
          REGISTRY="347788108263.dkr.ecr.us-east-1.amazonaws.com"
          
          NAMESPACES=("default" "josh-app" "teamchikynbitts-app")
          for NS in "${NAMESPACES[@]}"; do
            echo "Updating secret in namespace $NS"
            kubectl delete secret regcred -n $NS --ignore-not-found
            kubectl create secret docker-registry regcred -n $NS \
              --docker-server=$REGISTRY \
              --docker-username=AWS \
              --docker-password=$TOKEN
            kubectl patch serviceaccount default -n $NS -p '{"imagePullSecrets":[{"name":"regcred"}]}' || echo "SA not found in $NS, skipping patch"
          done
          echo "Done!"
      restartPolicy: Never
`
		_, err = yaml.NewConfigGroup(ctx, "ecr-cron", &yaml.ConfigGroupArgs{
			YAML: []string{ecrCronYAML},
		}, pulumi.Provider(k8sProvider))
		if err != nil {
			return err
		}

		// 11. Flux GitRepository
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

		// Create cluster-vars ConfigMap for Flux variable substitution
		// This allows manifests to use ${PUBLIC_IP} which Flux will replace at reconcile time
		_, err = corev1.NewConfigMap(ctx, "cluster-vars", &corev1.ConfigMapArgs{
			Metadata: &metav1.ObjectMetaArgs{
				Name:      pulumi.String("cluster-vars"),
				Namespace: pulumi.String("flux-system"),
			},
			Data: pulumi.StringMap{
				"PUBLIC_IP": eip.PublicIp,
			},
		}, pulumi.Provider(k8sProvider), pulumi.DependsOn([]pulumi.Resource{fluxRelease}))
		if err != nil {
			return err
		}

		// 10. Flux Kustomizations
		// App 1: TeamChikynbitts App
		// postBuild.substituteFrom reads PUBLIC_IP from cluster-vars ConfigMap
		app1YAML := `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: teamchikynbitts-app
  namespace: flux-system
spec:
  interval: 1m0s
  targetNamespace: teamchikynbitts-app
  sourceRef:
    kind: GitRepository
    name: teamchikynbitts-repo
  path: "./app/teamchikynbitts-app/k8s"
  prune: true
  wait: true
  postBuild:
    substituteFrom:
      - kind: ConfigMap
        name: cluster-vars
`

		// App 2: Josh App
		app2YAML := `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: josh-app
  namespace: flux-system
spec:
  interval: 1m0s
  targetNamespace: josh-app
  sourceRef:
    kind: GitRepository
    name: teamchikynbitts-repo
  path: "./app/josh-app/k8s"
  prune: true
  wait: true
  postBuild:
    substituteFrom:
      - kind: ConfigMap
        name: cluster-vars
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
