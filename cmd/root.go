package cmd

import (
	"fmt"
	"os"

	"github.com/hzbay/chain-bridge/cmd/db"
	"github.com/hzbay/chain-bridge/cmd/env"
	"github.com/hzbay/chain-bridge/cmd/probe"
	"github.com/hzbay/chain-bridge/cmd/server"
	"github.com/hzbay/chain-bridge/internal/config"
	"github.com/hzbay/chain-bridge/internal/queue"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Version: config.GetFormattedBuildArgs(),
	Use:     "app",
	Short:   config.ModuleName,
	Long: fmt.Sprintf(`%v

A stateless RESTful JSON service written in Go.
Requires configuration through ENV.`, config.ModuleName),
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.SetVersionTemplate(`{{printf "%s\n" .Version}}`)

	// attach the subcommands
	rootCmd.AddCommand(
		db.New(),
		env.New(),
		probe.New(),
		server.New(),
		newRabbitMQCmd(),
	)
}

// newRabbitMQCmd creates rabbitmq management command
func newRabbitMQCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rabbitmq",
		Short: "RabbitMQ queue management",
		Long:  "Manage RabbitMQ queues, connections and debug issues",
	}

	// Add subcommands
	cmd.AddCommand(
		newRabbitMQStatusCmd(),
		newRabbitMQFixCmd(),
	)

	return cmd
}

func newRabbitMQStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check RabbitMQ connection and queue status",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println("🔍 Checking RabbitMQ status...")

			cfg := config.DefaultServiceConfigFromEnv()
			client, err := queue.NewRabbitMQClient(cfg.RabbitMQ)
			if err != nil {
				fmt.Printf("❌ Failed to connect to RabbitMQ: %v\n", err)
				return
			}
			defer client.Close()

			if client.IsHealthy() {
				fmt.Println("✅ RabbitMQ connection is healthy")
			} else {
				fmt.Println("❌ RabbitMQ connection is unhealthy")
			}

			// Check essential queues
			essentialQueues := []string{
				fmt.Sprintf("%s.notification.0.0", cfg.RabbitMQ.QueuePrefix),
				fmt.Sprintf("%s.transfer.11155111.1", cfg.RabbitMQ.QueuePrefix),
				fmt.Sprintf("%s.asset_adjust.11155111.1", cfg.RabbitMQ.QueuePrefix),
				fmt.Sprintf("%s.health_check.999999.999999", cfg.RabbitMQ.QueuePrefix), // Health check queue
			}

			fmt.Println("\n📋 Queue Status:")
			for _, queueName := range essentialQueues {
				count, err := client.GetQueueInfo(queueName)
				if err != nil {
					fmt.Printf("❌ %s (ERROR: %v)\n", queueName, err)
				} else {
					fmt.Printf("✅ %s (%d messages)\n", queueName, count)
				}
			}
		},
	}
}

func newRabbitMQFixCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fix",
		Short: "Fix common RabbitMQ issues",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println("🔧 Fixing RabbitMQ issues...")

			cfg := config.DefaultServiceConfigFromEnv()
			client, err := queue.NewRabbitMQClient(cfg.RabbitMQ)
			if err != nil {
				fmt.Printf("❌ Failed to connect to RabbitMQ: %v\n", err)
				return
			}
			defer client.Close()

			// Create essential queues
			essentialQueues := []string{
				fmt.Sprintf("%s.notification.0.0", cfg.RabbitMQ.QueuePrefix),
				fmt.Sprintf("%s.transfer.11155111.1", cfg.RabbitMQ.QueuePrefix),
				fmt.Sprintf("%s.asset_adjust.11155111.1", cfg.RabbitMQ.QueuePrefix),
			}

			fmt.Println("Creating missing queues...")
			for _, queueName := range essentialQueues {
				_, err := client.DeclareQueue(queueName)
				if err != nil {
					fmt.Printf("❌ Failed to create %s: %v\n", queueName, err)
				} else {
					fmt.Printf("✅ Created/verified queue: %s\n", queueName)
				}
			}

			fmt.Println("✅ RabbitMQ fix completed!")
		},
	}
}
