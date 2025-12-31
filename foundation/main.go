package main

import (
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/budgets"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Define Users
		users := []string{"Joshua Hayes", "Justin Rouse", "Abby Adkins"}
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
		}

		// Create Budget Alert: $50
		_, err := budgets.NewBudget(ctx, "budget-50", &budgets.BudgetArgs{
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

		return nil
	})
}
