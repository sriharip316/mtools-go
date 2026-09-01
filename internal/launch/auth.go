package launch

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// GenerateKeyfile creates a 753-byte base64 keyfile with 0600 permissions.
func GenerateKeyfile(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory for keyfile %s: %w", dir, err)
	}

	keyfilePath := filepath.Join(dir, "keyfile")

	// If keyfile already exists, reuse it
	if _, err := os.Stat(keyfilePath); err == nil {
		return keyfilePath, nil
	}

	raw := make([]byte, 753)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate random keyfile bytes: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(raw)
	if err := os.WriteFile(keyfilePath, []byte(encoded+"\n"), 0600); err != nil {
		return "", fmt.Errorf("failed to write keyfile %s: %w", keyfilePath, err)
	}

	return keyfilePath, nil
}

// CreateAdminUser creates the administrative user on the specified port.
func CreateAdminUser(ctx context.Context, host string, port int, username, password, authDB string, roles []string) error {
	if authDB == "" {
		authDB = "admin"
	}
	if username == "" {
		username = "user"
	}
	if password == "" {
		password = "password"
	}
	if len(roles) == 0 {
		roles = []string{"dbAdminAnyDatabase", "readWriteAnyDatabase", "userAdminAnyDatabase", "clusterAdmin"}
	}

	uri := fmt.Sprintf("mongodb://%s:%d/?directConnection=true", host, port)
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetTimeout(5 * time.Second))
	if err != nil {
		return fmt.Errorf("failed to connect to %s:%d to create admin user: %w", host, port, err)
	}
	defer client.Disconnect(ctx)

	// Build createUser command
	cmd := bson.D{
		{Key: "createUser", Value: username},
		{Key: "pwd", Value: password},
		{Key: "roles", Value: roles},
	}

	var result bson.M
	err = client.Database(authDB).RunCommand(ctx, cmd).Decode(&result)
	if err != nil {
		return fmt.Errorf("createUser command failed on %s:%d: %w", host, port, err)
	}

	return nil
}
