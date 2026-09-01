package launch

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// BuildReplSetDoc constructs the MongoDB replica set configuration document.
func BuildReplSetDoc(name string, host string, ports []int, arbiterPort *int, priority bool) bson.M {
	if host == "" {
		host = "localhost"
	}

	var members []bson.M

	for i, port := range ports {
		member := bson.M{
			"_id":  i,
			"host": fmt.Sprintf("%s:%d", host, port),
		}

		if i == 0 && priority {
			member["priority"] = 10
		}

		// MongoDB restricts replica sets to at most 7 voting members
		if i >= 7 {
			member["votes"] = 0
			member["priority"] = 0
		}

		members = append(members, member)
	}

	if arbiterPort != nil {
		members = append(members, bson.M{
			"_id":         len(members),
			"host":        fmt.Sprintf("%s:%d", host, *arbiterPort),
			"arbiterOnly": true,
		})
	}

	return bson.M{
		"_id":     name,
		"members": members,
	}
}

// InitiateReplSet connects to member 0 and runs the replSetInitiate command.
func InitiateReplSet(ctx context.Context, host string, port int, configDoc bson.M) error {
	uri := fmt.Sprintf("mongodb://%s:%d/?directConnection=true", host, port)
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return fmt.Errorf("failed to connect to %s:%d: %w", host, port, err)
	}
	defer client.Disconnect(ctx)

	cmd := bson.D{{Key: "replSetInitiate", Value: configDoc}}

	var res bson.M
	err = client.Database("admin").RunCommand(ctx, cmd).Decode(&res)
	if err != nil {
		return fmt.Errorf("replSetInitiate failed on %s:%d: %w", host, port, err)
	}

	return nil
}

// WaitForPrimary polls the replica set node until a primary is elected.
func WaitForPrimary(host string, port int, timeout time.Duration) error {
	start := time.Now()
	uri := fmt.Sprintf("mongodb://%s:%d/?directConnection=true", host, port)

	for {
		if time.Since(start) > timeout {
			return fmt.Errorf("timeout (%v) waiting for primary on %s:%d", timeout, host, port)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		client, err := mongo.Connect(options.Client().ApplyURI(uri))
		if err == nil {
			var hello bson.M
			err = client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&hello)
			client.Disconnect(ctx)
			cancel()

			if err == nil {
				if isWritablePrimary, ok := hello["isWritablePrimary"].(bool); ok && isWritablePrimary {
					return nil
				}
				if primary, ok := hello["primary"].(string); ok && primary != "" {
					return nil
				}
			}
		} else {
			cancel()
		}

		time.Sleep(1 * time.Second)
	}
}
