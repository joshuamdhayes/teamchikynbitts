package main

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/budgets"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ecr"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {

		// Read users from file
		// Note: users.json should be in the same directory as the Pulumi program
		fileContent, err := os.ReadFile("users.json")
		if err != nil {
			return err
		}

		type UserConfig struct {
			Name   string   `json:"name"`
			Groups []string `json:"groups"`
		}

		var users []UserConfig
		if err := json.Unmarshal(fileContent, &users); err != nil {
			return err
		}

		// Read bots from file
		botsFileContent, err := os.ReadFile("bots.json")
		if err != nil {
			return err
		}
		var bots []string
		if err := json.Unmarshal(botsFileContent, &bots); err != nil {
			return err
		}

		// Read config from file
		type Config struct {
			BudgetNotificationEmail string `json:"budget_notification_email"`
		}
		configContent, err := os.ReadFile("config.json")
		if err != nil {
			return err
		}
		var config Config
		if err := json.Unmarshal(configContent, &config); err != nil {
			return err
		}

		// Define Group Policies (Name -> Policy ARN)
		groupPolicies := map[string]string{
			"technical": "arn:aws:iam::aws:policy/AdministratorAccess", // Replaces old "sysadmins"
			"billing":   "arn:aws:iam::aws:policy/job-function/Billing",
		}

		iamGroups := make(map[string]*iam.Group)

		// Create Functional Groups
		for groupName, policyArn := range groupPolicies {
			g, err := iam.NewGroup(ctx, groupName, &iam.GroupArgs{
				Name: pulumi.String(groupName),
			})
			if err != nil {
				return err
			}
			iamGroups[groupName] = g

			// Attach Policy
			_, err = iam.NewGroupPolicyAttachment(ctx, groupName+"-policy", &iam.GroupPolicyAttachmentArgs{
				Group:     g.Name,
				PolicyArn: pulumi.String(policyArn),
			})
			if err != nil {
				return err
			}
		}

		for _, userCfg := range users {
			// Sanitize name for resource name (spaces to dashes, lowercase)
			resourceName := strings.ReplaceAll(strings.ToLower(userCfg.Name), " ", "-")

			user, err := iam.NewUser(ctx, "user-"+resourceName, &iam.UserArgs{
				Name: pulumi.String(resourceName), // AWS does NOT allow spaces
				Tags: pulumi.StringMap{
					"ManagedBy": pulumi.String("Pulumi"),
					"Team":      pulumi.String("TeamChikynbitts"),
				},
			})
			if err != nil {
				return err
			}
			ctx.Export("UserARN-"+resourceName, user.Arn)

			// Create Access Keys
			key, err := iam.NewAccessKey(ctx, "key-"+resourceName, &iam.AccessKeyArgs{
				User: user.Name,
			})
			if err != nil {
				return err
			}
			ctx.Export("AccessKeyId-"+resourceName, key.ID())
			ctx.Export("SecretAccessKey-"+resourceName, key.Secret)

			// Create User Login Profile (Enables Console Access)
			profile, err := iam.NewUserLoginProfile(ctx, "profile-"+resourceName, &iam.UserLoginProfileArgs{
				User:                  user.Name,
				PasswordResetRequired: pulumi.Bool(true),
			})
			if err != nil {
				return err
			}
			ctx.Export("ConsolePassword-"+resourceName, profile.Password)

			// Collect user names for group membership (for MFA enforcement mainly)

			// Add to Groups defined in JSON
			for _, gName := range userCfg.Groups {
				if group, ok := iamGroups[gName]; ok {
					_, err := iam.NewUserGroupMembership(ctx, "membership-"+resourceName+"-"+gName, &iam.UserGroupMembershipArgs{
						User: user.Name,
						Groups: pulumi.StringArray{
							group.Name,
						},
					})
					if err != nil {
						return err
					}
				}
			}
		}

		var botNames []string

		for _, name := range bots {
			// Sanitize name for resource name (spaces to dashes, lowercase)
			resourceName := strings.ReplaceAll(strings.ToLower(name), " ", "-")
			resourceName = "bot-" + resourceName

			user, err := iam.NewUser(ctx, resourceName, &iam.UserArgs{
				Name: pulumi.String(resourceName),
				Tags: pulumi.StringMap{
					"ManagedBy": pulumi.String("Pulumi"),
					"Team":      pulumi.String("TeamChikynbitts"),
					"Type":      pulumi.String("Bot"),
				},
			})
			if err != nil {
				return err
			}
			ctx.Export("UserARN-"+resourceName, user.Arn)

			// Create Access Keys
			key, err := iam.NewAccessKey(ctx, "key-"+resourceName, &iam.AccessKeyArgs{
				User: user.Name,
			})
			if err != nil {
				return err
			}
			ctx.Export("AccessKeyId-"+resourceName, key.ID())
			ctx.Export("SecretAccessKey-"+resourceName, key.Secret)

			// Collect bot names for group membership
			botNames = append(botNames, resourceName)
		}

		// Create Bots Group
		botsGroup, err := iam.NewGroup(ctx, "bots", &iam.GroupArgs{
			Name: pulumi.String("bots"),
		})
		if err != nil {
			return err
		}

		// Add Bots to Group
		for _, bName := range botNames {
			_, err := iam.NewUserGroupMembership(ctx, "membership-"+bName, &iam.UserGroupMembershipArgs{
				User: pulumi.String(bName),
				Groups: pulumi.StringArray{
					botsGroup.Name,
				},
			})
			if err != nil {
				return err
			}
		}

		// Attach Administrator Access to Bots Group
		_, err = iam.NewGroupPolicyAttachment(ctx, "bot-admin-access", &iam.GroupPolicyAttachmentArgs{
			Group:     botsGroup.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AdministratorAccess"),
		})
		if err != nil {
			return err
		}

		// Note: "sysadmins" group is replaced by "technical" group logic above.
		// However, we still need to attach MFA enforcement to these users.
		// We can attach it to the "technical" group or iterate users.
		// The original code created a "sysadmins" group and put everyone in it.
		// Since we now have split groups, we should probably ensure MFA is enforced on 'technical' users at least.
		// Better yet, let's create a "common-users" group or just attach MFA to "technical".
		// For now, let's attach MFA enforcement to the "technical" group as that covers all users currently.

		// Set Account Password Policy
		_, err = iam.NewAccountPasswordPolicy(ctx, "password-policy", &iam.AccountPasswordPolicyArgs{
			MinimumPasswordLength:      pulumi.Int(12),
			RequireNumbers:             pulumi.Bool(true),
			RequireSymbols:             pulumi.Bool(true),
			RequireLowercaseCharacters: pulumi.Bool(true),
			RequireUppercaseCharacters: pulumi.Bool(true),
			AllowUsersToChangePassword: pulumi.Bool(true),
			HardExpiry:                 pulumi.Bool(false),
			MaxPasswordAge:             pulumi.Int(90),
			PasswordReusePrevention:    pulumi.Int(3),
		})
		if err != nil {
			return err
		}

		// Define MFA Enforcement Policy
		mfaPolicyJSON := map[string]interface{}{
			"Version": "2012-10-17",
			"Statement": []map[string]interface{}{
				{
					"Sid":    "AllowViewAccountInfo",
					"Effect": "Allow",
					"Action": []string{
						"iam:GetAccountPasswordPolicy",
						"iam:GetAccountSummary",
						"iam:ListVirtualMFADevices",
						"iam:ListUsers",
					},
					"Resource": "*",
				},
				{
					"Sid":    "AllowManageOwnPasswordsAndMFA",
					"Effect": "Allow",
					"Action": []string{
						"iam:ChangePassword",
						"iam:GetUser",
						"iam:CreateVirtualMFADevice",
						"iam:EnableMFADevice",
						"iam:ResyncMFADevice",
						"iam:DeleteVirtualMFADevice",
						"iam:ListMFADevices",
					},
					"Resource": []string{
						"arn:aws:iam::*:user/${aws:username}",
						"arn:aws:iam::*:mfa/${aws:username}",
					},
				},
				{
					"Sid":    "DenyAllExceptListedIfNoMFA",
					"Effect": "Deny",
					"NotAction": []string{
						"iam:GetAccountPasswordPolicy",
						"iam:GetAccountSummary",
						"iam:ListVirtualMFADevices",
						"iam:ChangePassword",
						"iam:GetUser",
						"iam:CreateVirtualMFADevice",
						"iam:EnableMFADevice",
						"iam:ResyncMFADevice",
						"iam:DeleteVirtualMFADevice",
						"iam:ListMFADevices",
						"iam:ListUsers",
					},
					"Resource": "*",
					"Condition": map[string]interface{}{
						"BoolIfExists": map[string]interface{}{
							"aws:MultiFactorAuthPresent": "false",
						},
					},
				},
			},
		}

		mfaPolicy, err := iam.NewPolicy(ctx, "mfa-enforcement", &iam.PolicyArgs{
			Name:        pulumi.String("EnforceMFA"),
			Description: pulumi.String("Requires MFA for all actions except MFA setup."),
			Policy:      pulumi.Any(mfaPolicyJSON),
		})
		if err != nil {
			return err
		}

		// Attach MFA Policy to ALL functional Groups
		for name, g := range iamGroups {
			_, err = iam.NewGroupPolicyAttachment(ctx, "mfa-enforcement-attach-"+name, &iam.GroupPolicyAttachmentArgs{
				Group:     g.Name,
				PolicyArn: mfaPolicy.Arn,
			})
			if err != nil {
				return err
			}
		}

		// Create Budget Alert: $50
		_, err = budgets.NewBudget(ctx, "budget-50", &budgets.BudgetArgs{
			BudgetType:      pulumi.String("COST"),
			LimitAmount:     pulumi.String("50.0"),
			LimitUnit:       pulumi.String("USD"),
			TimePeriodStart: pulumi.String("2024-01-01_00:00"),
			TimeUnit:        pulumi.String("MONTHLY"),
			Notifications: budgets.BudgetNotificationArray{
				&budgets.BudgetNotificationArgs{
					ComparisonOperator:       pulumi.String("GREATER_THAN"),
					Threshold:                pulumi.Float64(80), // Alert at 80% ($40)
					ThresholdType:            pulumi.String("PERCENTAGE"),
					NotificationType:         pulumi.String("ACTUAL"),
					SubscriberEmailAddresses: pulumi.StringArray{pulumi.String(config.BudgetNotificationEmail)},
				},
				&budgets.BudgetNotificationArgs{
					ComparisonOperator:       pulumi.String("GREATER_THAN"),
					Threshold:                pulumi.Float64(100), // Alert at 100% ($50)
					ThresholdType:            pulumi.String("PERCENTAGE"),
					NotificationType:         pulumi.String("FORECASTED"), // Forecast to exceed
					SubscriberEmailAddresses: pulumi.StringArray{pulumi.String(config.BudgetNotificationEmail)},
				},
			},
		})
		if err != nil {
			return err
		}

		// Create Budget Alert: $75 (Critical)
		_, err = budgets.NewBudget(ctx, "budget-75", &budgets.BudgetArgs{
			BudgetType:      pulumi.String("COST"),
			LimitAmount:     pulumi.String("75.0"),
			LimitUnit:       pulumi.String("USD"),
			TimePeriodStart: pulumi.String("2024-01-01_00:00"),
			TimeUnit:        pulumi.String("MONTHLY"),
			Notifications: budgets.BudgetNotificationArray{
				&budgets.BudgetNotificationArgs{
					ComparisonOperator:       pulumi.String("GREATER_THAN"),
					Threshold:                pulumi.Float64(100), // Alert at 100% ($75)
					ThresholdType:            pulumi.String("PERCENTAGE"),
					NotificationType:         pulumi.String("ACTUAL"),
					SubscriberEmailAddresses: pulumi.StringArray{pulumi.String(config.BudgetNotificationEmail)},
				},
			},
		})
		if err != nil {
			return err
		}

		// Create ECR Repository for Team Chikynbitts App
		repo, err := ecr.NewRepository(ctx, "teamchikynbitts-app-repo", &ecr.RepositoryArgs{
			Name:               pulumi.String("teamchikynbitts-app"),
			ImageTagMutability: pulumi.String("MUTABLE"),
			ImageScanningConfiguration: &ecr.RepositoryImageScanningConfigurationArgs{
				ScanOnPush: pulumi.Bool(true),
			},
		})
		if err != nil {
			return err
		}
		ctx.Export("RepositoryURL-teamchikynbitts-app", repo.RepositoryUrl)

		// Create ECR Repository for Josh App
		joshRepo, err := ecr.NewRepository(ctx, "josh-app-repo", &ecr.RepositoryArgs{
			Name:               pulumi.String("josh-app"),
			ImageTagMutability: pulumi.String("MUTABLE"),
			ImageScanningConfiguration: &ecr.RepositoryImageScanningConfigurationArgs{
				ScanOnPush: pulumi.Bool(true),
			},
		})
		if err != nil {
			return err
		}
		ctx.Export("RepositoryURL-josh-app", joshRepo.RepositoryUrl)

		return nil
	})
}
