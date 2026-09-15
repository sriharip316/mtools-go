package launch

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// AddShard registers a shard with the mongos router.
func AddShard(mongosHost string, mongosPort int, shardConnStr string, shardName string, timeout time.Duration) error {
	start := time.Now()
	uri := fmt.Sprintf("mongodb://%s:%d/?directConnection=true", mongosHost, mongosPort)

	for {
		if time.Since(start) > timeout {
			return fmt.Errorf("timeout (%v) adding shard %s (%s) via mongos on %s:%d", timeout, shardName, shardConnStr, mongosHost, mongosPort)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		client, err := mongo.Connect(options.Client().ApplyURI(uri))
		if err == nil {
			cmd := bson.D{
				{Key: "addShard", Value: shardConnStr},
				{Key: "name", Value: shardName},
			}
			var res bson.M
			err = client.Database("admin").RunCommand(ctx, cmd).Decode(&res)
			_ = client.Disconnect(ctx)
			cancel()

			if err == nil {
				if ok, _ := res["ok"].(float64); ok == 1 {
					return nil
				}
				if ok, _ := res["ok"].(int32); ok == 1 {
					return nil
				}
			}
		} else {
			cancel()
		}

		time.Sleep(1 * time.Second)
	}
}

// WaitForShards polls mongos until all expected shards are registered in config.shards.
func WaitForShards(mongosHost string, mongosPort int, expectedCount int, timeout time.Duration) error {
	start := time.Now()
	uri := fmt.Sprintf("mongodb://%s:%d/?directConnection=true", mongosHost, mongosPort)

	for {
		if time.Since(start) > timeout {
			return fmt.Errorf("timeout (%v) waiting for %d shards in cluster via mongos %s:%d", timeout, expectedCount, mongosHost, mongosPort)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		client, err := mongo.Connect(options.Client().ApplyURI(uri))
		if err == nil {
			count, err := client.Database("config").Collection("shards").CountDocuments(ctx, bson.M{})
			_ = client.Disconnect(ctx)
			cancel()

			if err == nil && int(count) >= expectedCount {
				return nil
			}
		} else {
			cancel()
		}

		time.Sleep(1 * time.Second)
	}
}
