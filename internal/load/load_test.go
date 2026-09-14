package load

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestDefaultSchema(t *testing.T) {
	cfg := DefaultSchema()
	if cfg.Database != "test" {
		t.Fatalf("expected default database 'test', got %s", cfg.Database)
	}
	if len(cfg.Collections) != 2 {
		t.Fatalf("expected 2 default collections, got %d", len(cfg.Collections))
	}

	users := cfg.Collections[0]
	if users.Name != "users" {
		t.Errorf("expected collection 0 name 'users', got %s", users.Name)
	}
	if users.Weight != 1.0 {
		t.Errorf("expected users weight 1.0, got %f", users.Weight)
	}
	if users.CRUD.Read != 60 || users.CRUD.Create != 10 {
		t.Errorf("unexpected users CRUD weights: %+v", users.CRUD)
	}

	orders := cfg.Collections[1]
	if orders.Name != "orders" {
		t.Errorf("expected collection 1 name 'orders', got %s", orders.Name)
	}
	if orders.Weight != 3.0 {
		t.Errorf("expected orders weight 3.0, got %f", orders.Weight)
	}
}

func TestParseSchemaMultiCollectionArray(t *testing.T) {
	rawJSON := []byte(`{
		"database": "shop",
		"collections": [
			{
				"name": "products",
				"weight": 2.5,
				"crud": {"create": 10, "read": 70, "update": 15, "delete": 5},
				"schema": {
					"type": "object",
					"properties": {
						"title": {"type": "string", "faker": "product"},
						"price": {"type": "number", "minimum": 10, "maximum": 500}
					},
					"required": ["title", "price"]
				}
			},
			{
				"name": "reviews",
				"weight": 5.0,
				"schema": {
					"type": "object",
					"properties": {
						"rating": {"type": "integer", "minimum": 1, "maximum": 5}
					}
				}
			}
		]
	}`)

	cfg, err := ParseSchema(rawJSON, "fallback")
	if err != nil {
		t.Fatalf("unexpected error parsing schema: %v", err)
	}
	if cfg.Database != "shop" {
		t.Errorf("expected db 'shop', got %s", cfg.Database)
	}
	if len(cfg.Collections) != 2 {
		t.Fatalf("expected 2 collections, got %d", len(cfg.Collections))
	}
	if cfg.Collections[0].Name != "products" || cfg.Collections[0].Weight != 2.5 {
		t.Errorf("unexpected products collection: %+v", cfg.Collections[0])
	}
	if cfg.Collections[1].Name != "reviews" || cfg.Collections[1].Weight != 5.0 {
		t.Errorf("unexpected reviews collection: %+v", cfg.Collections[1])
	}
}

func TestParseSchemaSingleCollection(t *testing.T) {
	rawJSON := []byte(`{
		"title": "events",
		"type": "object",
		"properties": {
			"eventType": {"type": "string", "enum": ["click", "view", "purchase"]},
			"timestamp": {"type": "string", "format": "date-time"}
		}
	}`)

	cfg, err := ParseSchema(rawJSON, "analytics")
	if err != nil {
		t.Fatalf("unexpected error parsing single collection: %v", err)
	}
	if cfg.Database != "analytics" {
		t.Errorf("expected fallback db 'analytics', got %s", cfg.Database)
	}
	if len(cfg.Collections) != 1 {
		t.Fatalf("expected 1 collection, got %d", len(cfg.Collections))
	}
	if cfg.Collections[0].Name != "events" {
		t.Errorf("expected collection name 'events', got %s", cfg.Collections[0].Name)
	}
}

func TestGeneratorDataTypes(t *testing.T) {
	gen := NewGenerator()
	schema := &JSONSchema{
		Type: "object",
		Properties: map[string]*JSONSchema{
			"_id":       {Type: "objectId"},
			"name":      {Type: "string", Faker: "name"},
			"email":     {Type: "string", Format: "email"},
			"uuid":      {Type: "string", Format: "uuid"},
			"ipv4":      {Type: "string", Format: "ipv4"},
			"age":       {Type: "integer", Minimum: new(float64(20)), Maximum: new(float64(40))},
			"balance":   {Type: "number", Minimum: new(100.5), Maximum: new(500.5)},
			"active":    {Type: "boolean"},
			"status":    {Type: "string", Enum: []any{"A", "B", "C"}},
			"tags":      {Type: "array", MinItems: new(2), MaxItems: new(5), Items: &JSONSchema{Type: "string", Faker: "word"}},
			"createdAt": {Type: "string", Format: "date-time"},
			"profile": {
				Type: "object",
				Properties: map[string]*JSONSchema{
					"bio": {Type: "string", Faker: "sentence"},
				},
				Required: []string{"bio"},
			},
		},
		Required: []string{"_id", "name", "email", "age", "active", "status", "profile"},
	}

	for range 20 {
		doc := gen.GenerateDocument(schema)
		if doc == nil {
			t.Fatalf("generated document is nil")
		}

		if _, ok := doc["_id"].(bson.ObjectID); !ok {
			t.Errorf("expected _id to be bson.ObjectID, got %T (%v)", doc["_id"], doc["_id"])
		}
		if name, ok := doc["name"].(string); !ok || len(name) == 0 {
			t.Errorf("expected name to be non-empty string, got %v", doc["name"])
		}
		if email, ok := doc["email"].(string); !ok || len(email) == 0 {
			t.Errorf("expected email to be non-empty string, got %v", doc["email"])
		}
		if age, ok := doc["age"].(int64); !ok || age < 20 || age > 40 {
			t.Errorf("expected age in [20, 40], got %v (%T)", doc["age"], doc["age"])
		}
		if balance, ok := doc["balance"].(float64); ok {
			if balance < 100.5 || balance > 500.5 {
				t.Errorf("expected balance in [100.5, 500.5], got %f", balance)
			}
		}
		if status, ok := doc["status"].(string); !ok || (status != "A" && status != "B" && status != "C") {
			t.Errorf("expected status in [A, B, C], got %v", doc["status"])
		}
		if tags, ok := doc["tags"].(bson.A); ok {
			if len(tags) < 2 || len(tags) > 5 {
				t.Errorf("expected tags length in [2, 5], got %d", len(tags))
			}
		}
		if profile, ok := doc["profile"].(bson.M); !ok || profile["bio"] == nil {
			t.Errorf("expected profile.bio to exist, got %v", doc["profile"])
		}
	}
}

func TestGeneratorUpdateAndMissQueries(t *testing.T) {
	gen := NewGenerator()
	schema := DefaultSchema().Collections[0].Schema

	patch := gen.GenerateUpdatePatch(schema)
	setObj, ok := patch["$set"].(bson.M)
	if !ok || len(setObj) == 0 {
		t.Fatalf("expected valid $set in update patch: %+v", patch)
	}

	// Verify immutable fields are never in update patch
	for _, forbidden := range []string{"_id", "id", "createdAt"} {
		if _, exists := setObj[forbidden]; exists {
			t.Errorf("update patch contains forbidden immutable field %s", forbidden)
		}
	}

	missQuery := gen.GenerateMissQuery()
	if missQuery["_id"] == nil {
		t.Fatalf("expected miss query to have _id, got %+v", missQuery)
	}
}

func TestWorkingSet(t *testing.T) {
	ws := NewWorkingSet(100)

	// Add 50 items
	var ids []bson.ObjectID
	for range 50 {
		id := bson.NewObjectID()
		ids = append(ids, id)
		ws.Add("users", id)
	}

	if ws.Size("users") != 50 {
		t.Fatalf("expected size 50, got %d", ws.Size("users"))
	}

	// Sample
	sampled, ok := ws.Sample("users")
	if !ok || sampled == nil {
		t.Fatalf("expected successful sample, got %v", sampled)
	}

	// Remove
	removed := ws.Remove("users", ids[0])
	if !removed {
		t.Fatalf("expected removal to succeed for %v", ids[0])
	}
	if ws.Size("users") != 49 {
		t.Fatalf("expected size 49 after removal, got %d", ws.Size("users"))
	}

	// Ensure capacity bound
	smallWS := NewWorkingSet(5)
	for range 20 {
		smallWS.Add("orders", bson.NewObjectID())
	}
	if smallWS.Size("orders") != 5 {
		t.Fatalf("expected smallWS size bounded at 5, got %d", smallWS.Size("orders"))
	}
}

func TestCollectionSampler(t *testing.T) {
	colls := []CollectionConfig{
		{Name: "users", Weight: 1.0},
		{Name: "orders", Weight: 3.0},
	}

	sampler := NewCollectionSampler(colls)
	counts := make(map[string]int)

	const samples = 10000
	for range samples {
		c := sampler.Sample()
		counts[c.Name]++
	}

	usersRatio := float64(counts["users"]) / float64(samples)
	ordersRatio := float64(counts["orders"]) / float64(samples)

	// 1:3 ratio => ~0.25 users and ~0.75 orders (allow ±0.05 tolerance)
	if usersRatio < 0.20 || usersRatio > 0.30 {
		t.Errorf("expected users ratio ~0.25, got %f", usersRatio)
	}
	if ordersRatio < 0.70 || ordersRatio > 0.80 {
		t.Errorf("expected orders ratio ~0.75, got %f", ordersRatio)
	}
}

func TestCRUDSampler(t *testing.T) {
	weights := CRUDWeights{
		Create: 10,
		Read:   60,
		Update: 25,
		Delete: 5,
	}

	sampler := NewCRUDSampler(weights)
	counts := make(map[OpType]int)

	const samples = 10000
	for range samples {
		op := sampler.Sample()
		counts[op]++
	}

	readRatio := float64(counts[OpRead]) / float64(samples)
	createRatio := float64(counts[OpCreate]) / float64(samples)

	// 60% Read (allow ±0.05 tolerance)
	if readRatio < 0.55 || readRatio > 0.65 {
		t.Errorf("expected read ratio ~0.60, got %f", readRatio)
	}
	// 10% Create
	if createRatio < 0.07 || createRatio > 0.13 {
		t.Errorf("expected create ratio ~0.10, got %f", createRatio)
	}
}

func TestRateLimiter(t *testing.T) {
	rl := NewRateLimiter(50, 100)
	defer rl.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	count := 0
	for {
		if err := rl.Wait(ctx); err != nil {
			break
		}
		count++
	}

	if count < 5 {
		t.Fatalf("expected at least 5 tokens in 200ms at 50-100 ops/s, got %d", count)
	}
}

func TestStatsTracker(t *testing.T) {
	st := NewStatsTracker()

	// Initial Load stats
	st.RecordInitLoad("users", 2*time.Millisecond, 10)
	st.RecordInitLoad("orders", 3*time.Millisecond, 30)

	if st.initLoadDocs != 40 {
		t.Errorf("expected 40 init load docs, got %d", st.initLoadDocs)
	}

	// Switch to Phase 2
	st.SetPhase2()

	// Record CRUD ops
	st.RecordCRUD("users", OpCreate, 5*time.Millisecond, true, nil)
	st.RecordCRUD("users", OpRead, 1*time.Millisecond, true, nil)
	st.RecordCRUD("orders", OpRead, 2*time.Millisecond, false, nil) // miss
	st.RecordCRUD("orders", OpUpdate, 4*time.Millisecond, true, nil)
	st.RecordCRUD("orders", OpDelete, 3*time.Millisecond, true, nil)

	if st.createOps != 1 || st.readHits != 1 || st.readMisses != 1 || st.updateOps != 1 || st.deleteOps != 1 {
		t.Errorf("unexpected CRUD counts: C:%d, RH:%d, RM:%d, U:%d, D:%d",
			st.createOps, st.readHits, st.readMisses, st.updateOps, st.deleteOps)
	}

	// Check JSON interval report
	st.IntervalReport(50.0, false)
}

func TestConcurrentWorkingSet(t *testing.T) {
	ws := NewWorkingSet(1000)
	var wg sync.WaitGroup

	for i := range 8 {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := range 500 {
				id := bson.NewObjectID()
				ws.Add("users", id)
				_, _ = ws.Sample("users")
				if j%5 == 0 {
					ws.Remove("users", id)
				}
			}
		}(i)
	}

	wg.Wait()
	if ws.Size("users") <= 0 {
		t.Errorf("expected non-zero size after concurrent operations")
	}
}

func TestDryRunExecution(t *testing.T) {
	schemaCfg := DefaultSchema()
	opts := SimulationOptions{
		LoadDuration: 2 * time.Second,
		Duration:     2 * time.Second,
		DryRun:       true,
	}

	err := runDryRun(schemaCfg, opts)
	if err != nil {
		t.Fatalf("unexpected error running dry-run: %v", err)
	}
}
