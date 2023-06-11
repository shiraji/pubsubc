package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"cloud.google.com/go/pubsub"
	"google.golang.org/api/iterator"
)

var (
	debug   = flag.Bool("debug", false, "Enable debug logging")
	help    = flag.Bool("help", false, "Display usage information")
	version = flag.Bool("version", false, "Display version information")
)

// The CommitHash and Revision variables are set during building.
var (
	CommitHash = "<not set>"
	Revision   = "<not set>"
)

// Topics describes a PubSub topic and its subscriptions.
type Topics map[string][]string

func versionString() string {
	return fmt.Sprintf("pubsubc - build %s (%s) running on %s", Revision, CommitHash, runtime.Version())
}

// debugf prints debugging information.
func debugf(format string, params ...interface{}) {
	if *debug {
		fmt.Printf(format+"\n", params...)
	}
}

// fatalf prints an error to stderr and exits.
func fatalf(format string, params ...interface{}) {
	fmt.Fprintf(os.Stderr, os.Args[0]+": "+format+"\n", params...)
	os.Exit(1)
}

func displayResult(projectId string) {
	ctx := context.Background()
	client, err := pubsub.NewClient(ctx, projectId)
	if err != nil {
		return
	}
	defer client.Close()

	topics, err := listTopics(client)
	if err != nil {
		fatalf("Failed to list topics: %s", err)
		return
	}

	for _, topic := range topics {
		subscriptions, err := listSubscriptions(client, topic.ID())
		if err != nil {
			fatalf("Failed to list subscriptions: %s", err)
			return
		}

		debugf("Topic: %s\n", topic.ID())
		for _, sub := range subscriptions {
			for _, sub := range subscriptions {
				config, err := sub.Config(context.Background())
				if err != nil {
					fatalf("Failed to get subscription config %s", err)
				}
				fmt.Printf("  Subscription: %s - Endpoint: %s\n", sub.ID(), config.PushConfig.Endpoint)
			}

			debugf("  Subscription: %s\n", sub.ID())
		}
	}
}

func listSubscriptions(client *pubsub.Client, topicID string) ([]*pubsub.Subscription, error) {
	var subs []*pubsub.Subscription
	ctx := context.Background()
	it := client.Topic(topicID).Subscriptions(ctx)
	for {
		sub, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("Next: %w", err)
		}
		subs = append(subs, sub)
	}
	return subs, nil
}

func listTopics(client *pubsub.Client) ([]*pubsub.Topic, error) {
	var topics []*pubsub.Topic
	ctx := context.Background()
	it := client.Topics(ctx)
	for {
		topic, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("Next: %w", err)
		}
		topics = append(topics, topic)
	}

	return topics, nil
}

// create a connection to the PubSub service and create topics and subscriptions
// for the specified project ID.
func create(ctx context.Context, projectID string, topics Topics) error {
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return fmt.Errorf("Unable to create client to project %q: %s", projectID, err)
	}
	defer client.Close()

	debugf("Client connected with project ID %q", projectID)

	for topicID, subscriptions := range topics {
		debugf("  Creating topic %q", topicID)
		topic, err := client.CreateTopic(ctx, topicID)
		if err != nil {
			return fmt.Errorf("Unable to create topic %q for project %q: %s", topicID, projectID, err)
		}

		for _, subscription := range subscriptions {
			subscriptionParts := strings.Split(subscription, "+")
			subscriptionID := strings.TrimSpace(subscriptionParts[0])
			var pushEndpoint string
			if len(subscriptionParts) > 1 {
				pushEndpoint = strings.Replace(strings.TrimSpace(subscriptionParts[1]), "|", ":", 1)
			} else {
				pushEndpoint = ""
			}
			if pushEndpoint != "" {
				endPointURL := "http://" + pushEndpoint
				debugf("    Creating subscription %q - endpoint %q", subscriptionID, endPointURL)
				pushConfig := pubsub.PushConfig{Endpoint: endPointURL}
				_, err = client.CreateSubscription(
					ctx,
					subscriptionID,
					pubsub.SubscriptionConfig{Topic: topic, PushConfig: pushConfig},
				)
				if err != nil {
					return fmt.Errorf("Unable to create push subscription %q on topic %q for project %q using push endpoint %q: %s", subscriptionID, topicID, projectID, pushEndpoint, err)
				}
			} else {
				debugf("    Creating subscription %q", subscriptionID)
				_, err = client.CreateSubscription(ctx, subscriptionID, pubsub.SubscriptionConfig{Topic: topic})
				if err != nil {
					return fmt.Errorf("Unable to create subscription %q on topic %q for project %q: %s", subscriptionID, topicID, projectID, err)
				}
			}
		}
	}

	return nil
}

func main() {
	flag.Parse()
	flag.Usage = func() {
		fmt.Printf(`Usage: env PUBSUB_PROJECT1="project1,topic1,topic2:subscription1,topic3:subscription2+enpoint1" %s`+"\n", os.Args[0])
		flag.PrintDefaults()
	}

	if *help {
		flag.Usage()
		return
	}

	if *version {
		fmt.Println(versionString())
		return
	}

	// Cycle over the numbered PUBSUB_PROJECT environment variables.
	for i := 1; ; i++ {
		// Fetch the enviroment variable. If it doesn't exist, break out.
		currentEnv := fmt.Sprintf("PUBSUB_PROJECT%d", i)
		env := os.Getenv(currentEnv)
		if env == "" {
			// If this is the first environment variable, print the usage info.
			if i == 1 {
				flag.Usage()
				os.Exit(1)
			}

			break
		}

		// Separate the projectID from the topic definitions.
		parts := strings.Split(env, ",")
		if len(parts) < 2 {
			fatalf("%s: Expected at least 1 topic to be defined", currentEnv)
		}

		// Separate the topicID from the subscription IDs.
		topics := make(Topics)
		for _, part := range parts[1:] {
			topicParts := strings.Split(part, ":")
			topics[topicParts[0]] = topicParts[1:]
		}

		// Create the project and all its topics and subscriptions.
		if err := create(context.Background(), parts[0], topics); err != nil {
			fatalf(err.Error())
		}

		displayResult(parts[0])
	}
}
