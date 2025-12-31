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
		var users []string
		if err := json.Unmarshal(fileContent, &users); err != nil {
			return err
		}

		var userNames []string

		for _, name := range users {
			// Sanitize name for resource name (spaces to dashes, lowercase)
			resourceName := strings.ReplaceAll(strings.ToLower(name), " ", "-")

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

			// Collect user names for group membership
			userNames = append(userNames, resourceName)
		}

		// Create SysAdmins Group
		group, err := iam.NewGroup(ctx, "sysadmins", &iam.GroupArgs{
			Name: pulumi.String("sysadmins"),
		})
		if err != nil {
			return err
		}

		// Add Users to Group
		for _, uName := range userNames {
			_, err := iam.NewUserGroupMembership(ctx, "membership-"+uName, &iam.UserGroupMembershipArgs{
				User: pulumi.String(uName),
				Groups: pulumi.StringArray{
					group.Name,
				},
			})
			if err != nil {
				return err
			}
		}

		// Attach Administrator Access to Group
		_, err = iam.NewGroupPolicyAttachment(ctx, "admin-access", &iam.GroupPolicyAttachmentArgs{
			Group:     group.Name,
			PolicyArn: pulumi.String("arn:aws:iam::aws:policy/AdministratorAccess"),
		})
		if err != nil {
			return err
		}

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

		// Attach MFA Policy to Group
		_, err = iam.NewGroupPolicyAttachment(ctx, "mfa-enforcement-attach", &iam.GroupPolicyAttachmentArgs{
			Group:     group.Name,
			PolicyArn: mfaPolicy.Arn,
		})
		if err != nil {
			return err
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
					SubscriberEmailAddresses: pulumi.StringArray{pulumi.String("admin@example.com")},
				},
				&budgets.BudgetNotificationArgs{
					ComparisonOperator:       pulumi.String("GREATER_THAN"),
					Threshold:                pulumi.Float64(100), // Alert at 100% ($50)
					ThresholdType:            pulumi.String("PERCENTAGE"),
					NotificationType:         pulumi.String("FORECASTED"), // Forecast to exceed
					SubscriberEmailAddresses: pulumi.StringArray{pulumi.String("admin@example.com")},
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
					SubscriberEmailAddresses: pulumi.StringArray{pulumi.String("admin@example.com")},
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
		ctx.Export("RepositoryURL", repo.RepositoryUrl)

		return nil
	})
}
