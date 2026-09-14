package load

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// SimulationOptions holds all configuration parameters for running a load simulation.
type SimulationOptions struct {
	URI          string
	Database     string
	SchemaFile   string
	LoadDuration time.Duration
	Duration     time.Duration
	MinRate      float64
	MaxRate      float64
	Interval     time.Duration
	BatchSize    int
	Workers      int
	Drop         bool
	DryRun       bool
	JSON         bool
}

// Run executes the complete 2-phase load simulation.
func Run(opts SimulationOptions) error {
	// 1. Resolve Schema
	var schemaCfg *SchemaConfig
	var err error

	if opts.SchemaFile != "" {
		schemaCfg, err = LoadSchemaFromFile(opts.SchemaFile, opts.Database)
		if err != nil {
			return fmt.Errorf("failed to load schema: %w", err)
		}
	} else {
		schemaCfg = DefaultSchema()
		if opts.Database != "" {
			schemaCfg.Database = opts.Database
		}
	}

	if len(schemaCfg.Collections) == 0 {
		return fmt.Errorf("no collections defined in schema")
	}

	dbName := schemaCfg.Database
	if dbName == "" {
		dbName = "test"
	}

	// 2. Handle Dry-Run Mode
	if opts.DryRun {
		return runDryRun(schemaCfg, opts)
	}

	// 3. Connect to MongoDB
	if opts.URI == "" {
		opts.URI = "mongodb://localhost:27017"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	clientOpts := options.Client().ApplyURI(opts.URI)
	client, err := mongo.Connect(clientOpts)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB at %s: %w", opts.URI, err)
	}
	defer func() {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Disconnect(disconnectCtx)
	}()

	// Ping database to verify connection
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	err = client.Ping(pingCtx, nil)
	pingCancel()
	if err != nil {
		return fmt.Errorf("failed to reach MongoDB at %s: %w", opts.URI, err)
	}

	db := client.Database(dbName)

	// 4. Drop collections if requested
	if opts.Drop {
		for _, coll := range schemaCfg.Collections {
			_ = db.Collection(coll.Name).Drop(ctx)
		}
	}

	// 5. Initialize Components
	stats := NewStatsTracker()
	workingSet := NewWorkingSet(100000)
	collSampler := NewCollectionSampler(schemaCfg.Collections)
	crudSamplers := make(map[string]*CRUDSampler)
	for _, coll := range schemaCfg.Collections {
		crudSamplers[coll.Name] = NewCRUDSampler(coll.CRUD)
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = 4
	}

	interval := opts.Interval
	if interval <= 0 {
		interval = 1 * time.Second
	}

	// 6. Start Live Reporter Ticker
	var rlRef *RateLimiter
	var rlMu sync.Mutex

	tickerDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-tickerDone:
				return
			case <-ticker.C:
				targetRate := 0.0
				rlMu.Lock()
				if rlRef != nil {
					targetRate = rlRef.CurrentTargetRate()
				}
				rlMu.Unlock()
				stats.IntervalReport(targetRate, opts.JSON)
			}
		}
	}()

	// 7. Phase 1: Initial Load
	if opts.LoadDuration > 0 && ctx.Err() == nil {
		if !opts.JSON {
			fmt.Printf("==> Starting Phase 1: Initial Load (Duration: %s, Workers: %d, DB: %s)\n",
				opts.LoadDuration, workers, dbName)
		}

		phase1Ctx, phase1Cancel := context.WithTimeout(ctx, opts.LoadDuration)
		runInitialLoad(phase1Ctx, db, schemaCfg, collSampler, workingSet, stats, workers, opts.BatchSize)
		phase1Cancel()

		if !opts.JSON {
			fmt.Printf("==> Phase 1 completed: %d documents seeded into working set.\n", stats.initLoadDocs)
		}
	}

	// 8. Phase 2: CRUD Simulation
	if ctx.Err() == nil {
		stats.SetPhase2()

		minRate := opts.MinRate
		maxRate := opts.MaxRate
		if minRate <= 0 && maxRate <= 0 {
			minRate, maxRate = 10, 50
		}

		rl := NewRateLimiter(minRate, maxRate)
		defer rl.Stop()

		rlMu.Lock()
		rlRef = rl
		rlMu.Unlock()

		durationStr := "infinite (until Ctrl+C)"
		var phase2Ctx context.Context
		var phase2Cancel context.CancelFunc

		if opts.Duration > 0 {
			durationStr = opts.Duration.String()
			phase2Ctx, phase2Cancel = context.WithTimeout(ctx, opts.Duration)
		} else {
			phase2Ctx, phase2Cancel = context.WithCancel(ctx)
		}
		defer phase2Cancel()

		if !opts.JSON {
			fmt.Printf("==> Starting Phase 2: CRUD Simulation (Duration: %s, Rate: %.0f-%.0f ops/s, Workers: %d)\n",
				durationStr, minRate, maxRate, workers)
		}

		runCRUDSimulation(phase2Ctx, db, schemaCfg, collSampler, crudSamplers, workingSet, rl, stats, workers)
	}

	close(tickerDone)

	// 9. Output Final Summary Report
	if !opts.JSON {
		stats.PrintSummary()
	}

	return nil
}

func runInitialLoad(ctx context.Context, db *mongo.Database, schemaCfg *SchemaConfig, collSampler *CollectionSampler, workingSet *WorkingSet, stats *StatsTracker, workers, batchSize int) {
	var wg sync.WaitGroup
	if batchSize <= 0 {
		batchSize = 1
	}

	for range workers {
		wg.Go(func() {
			gen := NewGenerator()

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				collCfg := collSampler.Sample()
				if collCfg == nil {
					return
				}

				coll := db.Collection(collCfg.Name)

				if batchSize == 1 {
					doc := gen.GenerateDocument(collCfg.Schema)
					start := time.Now()
					_, err := coll.InsertOne(ctx, doc)
					lat := time.Since(start)

					if err != nil {
						if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
							stats.RecordError(err)
						}
					} else {
						id := doc["_id"]
						workingSet.Add(collCfg.Name, id)
						stats.RecordInitLoad(collCfg.Name, lat, 1)
					}
				} else {
					docs := make([]any, batchSize)
					ids := make([]any, batchSize)
					for b := 0; b < batchSize; b++ {
						d := gen.GenerateDocument(collCfg.Schema)
						docs[b] = d
						ids[b] = d["_id"]
					}

					start := time.Now()
					_, err := coll.InsertMany(ctx, docs)
					lat := time.Since(start)

					if err != nil {
						if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
							stats.RecordError(err)
						}
					} else {
						for _, id := range ids {
							workingSet.Add(collCfg.Name, id)
						}
						stats.RecordInitLoad(collCfg.Name, lat, int64(batchSize))
					}
				}
			}
		})
	}

	wg.Wait()
}

func runCRUDSimulation(ctx context.Context, db *mongo.Database, schemaCfg *SchemaConfig, collSampler *CollectionSampler, crudSamplers map[string]*CRUDSampler, workingSet *WorkingSet, rl *RateLimiter, stats *StatsTracker, workers int) {
	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			gen := NewGenerator()

			for {
				if err := rl.Wait(ctx); err != nil {
					return
				}

				collCfg := collSampler.Sample()
				if collCfg == nil {
					return
				}

				crudSampler := crudSamplers[collCfg.Name]
				op := crudSampler.Sample()
				coll := db.Collection(collCfg.Name)

				switch op {
				case OpCreate:
					doc := gen.GenerateDocument(collCfg.Schema)
					start := time.Now()
					_, err := coll.InsertOne(ctx, doc)
					lat := time.Since(start)

					if err == nil {
						workingSet.Add(collCfg.Name, doc["_id"])
						stats.RecordCRUD(collCfg.Name, OpCreate, lat, true, nil)
					} else if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
						stats.RecordCRUD(collCfg.Name, OpCreate, lat, false, err)
					}

				case OpRead:
					// 85% Hit query, 15% Miss query
					isHitQuery := (gen.rng.Float64() < 0.85)
					var id any
					var ok bool

					if isHitQuery {
						id, ok = workingSet.Sample(collCfg.Name)
					}

					var filter bson.M
					if ok && id != nil {
						filter = bson.M{"_id": id}
					} else {
						filter = gen.GenerateMissQuery()
					}

					start := time.Now()
					var res bson.M
					err := coll.FindOne(ctx, filter).Decode(&res)
					lat := time.Since(start)

					if err == nil {
						stats.RecordCRUD(collCfg.Name, OpRead, lat, true, nil)
					} else if errors.Is(err, mongo.ErrNoDocuments) {
						// Successful cache/lookup miss (expected ~15%)
						stats.RecordCRUD(collCfg.Name, OpRead, lat, false, nil)
					} else if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
						stats.RecordCRUD(collCfg.Name, OpRead, lat, false, err)
					}

				case OpUpdate:
					id, ok := workingSet.Sample(collCfg.Name)
					if !ok || id == nil {
						// Fallback to Create if working set empty
						doc := gen.GenerateDocument(collCfg.Schema)
						start := time.Now()
						_, err := coll.InsertOne(ctx, doc)
						lat := time.Since(start)
						if err == nil {
							workingSet.Add(collCfg.Name, doc["_id"])
							stats.RecordCRUD(collCfg.Name, OpCreate, lat, true, nil)
						}
						continue
					}

					patch := gen.GenerateUpdatePatch(collCfg.Schema)
					start := time.Now()
					_, err := coll.UpdateOne(ctx, bson.M{"_id": id}, patch)
					lat := time.Since(start)

					if err == nil {
						stats.RecordCRUD(collCfg.Name, OpUpdate, lat, true, nil)
					} else if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
						stats.RecordCRUD(collCfg.Name, OpUpdate, lat, false, err)
					}

				case OpDelete:
					id, ok := workingSet.Sample(collCfg.Name)
					if !ok || id == nil {
						continue
					}

					start := time.Now()
					_, err := coll.DeleteOne(ctx, bson.M{"_id": id})
					lat := time.Since(start)

					if err == nil {
						workingSet.Remove(collCfg.Name, id)
						stats.RecordCRUD(collCfg.Name, OpDelete, lat, true, nil)
					} else if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
						stats.RecordCRUD(collCfg.Name, OpDelete, lat, false, err)
					}
				}
			}
		})
	}

	wg.Wait()
}

func runDryRun(schemaCfg *SchemaConfig, opts SimulationOptions) error {
	fmt.Printf("[Dry-Run] Target Database: %s\n", schemaCfg.Database)
	fmt.Printf("[Dry-Run] Initial Load Duration: %s, CRUD Simulation Duration: %s\n", opts.LoadDuration, opts.Duration)
	fmt.Println("--------------------------------------------------------------------------------")
	fmt.Println("Configured Collections & Schema Weights:")
	for _, c := range schemaCfg.Collections {
		fmt.Printf("  • Collection: %-12s | Weight: %-4.1f | CRUD Mix: [C: %.0f%%, R: %.0f%%, U: %.0f%%, D: %.0f%%]\n",
			c.Name, c.Weight, c.CRUD.Create, c.CRUD.Read, c.CRUD.Update, c.CRUD.Delete)
	}

	gen := NewGenerator()
	fmt.Println("\n================================================================================")
	fmt.Println("Sample Phase 1 (Initial Load) Documents:")
	fmt.Println("================================================================================")
	for _, c := range schemaCfg.Collections {
		fmt.Printf("\n--- Collection: %s ---\n", c.Name)
		doc := gen.GenerateDocument(c.Schema)
		jsonBytes, _ := json.MarshalIndent(doc, "", "  ")
		fmt.Println(string(jsonBytes))
	}

	fmt.Println("\n================================================================================")
	fmt.Println("Sample Phase 2 (CRUD Simulation) Operations:")
	fmt.Println("================================================================================")
	for _, c := range schemaCfg.Collections {
		fmt.Printf("\n--- Collection: %s CRUD Examples ---\n", c.Name)
		createDoc := gen.GenerateDocument(c.Schema)
		createJSON, _ := json.MarshalIndent(createDoc, "  ", "  ")
		fmt.Printf("  [CREATE] InsertOne:\n  %s\n\n", string(createJSON))

		fmt.Printf("  [READ HIT] FindOne({ _id: \"%v\" })\n", createDoc["_id"])
		fmt.Printf("  [READ MISS] FindOne({ _id: \"%v\" }) -> (Expect: mongo.ErrNoDocuments)\n\n", gen.GenerateMissQuery()["_id"])

		patch := gen.GenerateUpdatePatch(c.Schema)
		patchJSON, _ := json.MarshalIndent(patch, "  ", "  ")
		fmt.Printf("  [UPDATE] UpdateOne({ _id: \"%v\" }, %s)\n\n", createDoc["_id"], string(patchJSON))

		fmt.Printf("  [DELETE] DeleteOne({ _id: \"%v\" })\n", createDoc["_id"])
	}

	return nil
}
