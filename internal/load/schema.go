package load

import (
	"encoding/json"
	"fmt"
	"os"
)

// CRUDWeights configures the percentage/relative distribution of CRUD operations.
type CRUDWeights struct {
	Create float64 `json:"create"`
	Read   float64 `json:"read"`
	Update float64 `json:"update"`
	Delete float64 `json:"delete"`
}

// DefaultCRUDWeights returns realistic default CRUD operation ratios (20% C, 50% R, 25% U, 5% D).
func DefaultCRUDWeights() CRUDWeights {
	return CRUDWeights{
		Create: 20,
		Read:   50,
		Update: 25,
		Delete: 5,
	}
}

// JSONSchema represents a JSON schema definition with generator metadata.
type JSONSchema struct {
	Type                 any                    `json:"type,omitempty"` // string or []string
	Title                string                 `json:"title,omitempty"`
	Description          string                 `json:"description,omitempty"`
	Properties           map[string]*JSONSchema `json:"properties,omitempty"`
	Required             []string               `json:"required,omitempty"`
	Items                *JSONSchema            `json:"items,omitempty"`
	Enum                 []any                  `json:"enum,omitempty"`
	Minimum              *float64               `json:"minimum,omitempty"`
	Maximum              *float64               `json:"maximum,omitempty"`
	ExclusiveMinimum     *float64               `json:"exclusiveMinimum,omitempty"`
	ExclusiveMaximum     *float64               `json:"exclusiveMaximum,omitempty"`
	MinLength            *int                   `json:"minLength,omitempty"`
	MaxLength            *int                   `json:"maxLength,omitempty"`
	MinItems             *int                   `json:"minItems,omitempty"`
	MaxItems             *int                   `json:"maxItems,omitempty"`
	Format               string                 `json:"format,omitempty"`
	Faker                string                 `json:"faker,omitempty"`
	Default              any                    `json:"default,omitempty"`
	Pattern              string                 `json:"pattern,omitempty"`
	AdditionalProperties any                    `json:"additionalProperties,omitempty"`
}

// CollectionConfig represents configuration for a specific collection in the simulation.
type CollectionConfig struct {
	Name   string      `json:"name"`
	Weight float64     `json:"weight"`
	CRUD   CRUDWeights `json:"crud"`
	Schema *JSONSchema `json:"schema"`
}

// SchemaConfig represents the complete multi-collection schema configuration.
type SchemaConfig struct {
	Database    string             `json:"database,omitempty"`
	Collections []CollectionConfig `json:"collections"`
}

// rawCollectionConfig is used for parsing collections when specified as an array.
type rawCollectionConfig struct {
	Name    string       `json:"name"`
	Weight  *float64     `json:"weight,omitempty"`
	Ratio   *float64     `json:"ratio,omitempty"`
	Percent *float64     `json:"percent,omitempty"`
	CRUD    *CRUDWeights `json:"crud,omitempty"`
	Schema  *JSONSchema  `json:"schema,omitempty"`
	// Also support inline schema definition (if schema fields are directly inside the collection object)
	Type       any                    `json:"type,omitempty"`
	Properties map[string]*JSONSchema `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
}

// rawSchemaConfig is an intermediate struct for detecting various JSON schema layouts.
type rawSchemaConfig struct {
	Database    string          `json:"database,omitempty"`
	Collections json.RawMessage `json:"collections,omitempty"`
	// Single collection fields at root
	Type       any                    `json:"type,omitempty"`
	Title      string                 `json:"title,omitempty"`
	Properties map[string]*JSONSchema `json:"properties,omitempty"`
	Required   []string               `json:"required,omitempty"`
	CRUD       *CRUDWeights           `json:"crud,omitempty"`
	Weight     *float64               `json:"weight,omitempty"`
}

// DefaultSchema returns the default baked-in 2-collection schema (users and orders).
func DefaultSchema() *SchemaConfig {
	usersWeight := 1.0
	ordersWeight := 3.0

	return &SchemaConfig{
		Database: "test",
		Collections: []CollectionConfig{
			{
				Name:   "users",
				Weight: usersWeight,
				CRUD: CRUDWeights{
					Create: 10,
					Read:   60,
					Update: 25,
					Delete: 5,
				},
				Schema: &JSONSchema{
					Type: "object",
					Properties: map[string]*JSONSchema{
						"_id": {
							Type: "objectId",
						},
						"username": {
							Type:  "string",
							Faker: "username",
						},
						"email": {
							Type:   "string",
							Format: "email",
						},
						"name": {
							Type: "object",
							Properties: map[string]*JSONSchema{
								"first": {Type: "string", Faker: "firstName"},
								"last":  {Type: "string", Faker: "lastName"},
							},
							Required: []string{"first", "last"},
						},
						"age": {
							Type:    "integer",
							Minimum: new(float64(18)),
							Maximum: new(float64(75)),
						},
						"status": {
							Type: "string",
							Enum: []any{"active", "inactive", "pending", "suspended"},
						},
						"role": {
							Type: "string",
							Enum: []any{"customer", "admin", "moderator", "guest"},
						},
						"address": {
							Type: "object",
							Properties: map[string]*JSONSchema{
								"street":  {Type: "string", Faker: "street"},
								"city":    {Type: "string", Faker: "city"},
								"country": {Type: "string", Faker: "country"},
								"zipCode": {Type: "string", Faker: "zipCode"},
							},
						},
						"tags": {
							Type:     "array",
							MinItems: new(1),
							MaxItems: new(4),
							Items: &JSONSchema{
								Type: "string",
								Enum: []any{"premium", "beta-tester", "verified", "newsletter", "vip"},
							},
						},
						"createdAt": {
							Type:   "string",
							Format: "date-time",
						},
						"updatedAt": {
							Type:   "string",
							Format: "date-time",
						},
					},
					Required: []string{"username", "email", "name", "status", "createdAt"},
				},
			},
			{
				Name:   "orders",
				Weight: ordersWeight,
				CRUD: CRUDWeights{
					Create: 20,
					Read:   45,
					Update: 30,
					Delete: 5,
				},
				Schema: &JSONSchema{
					Type: "object",
					Properties: map[string]*JSONSchema{
						"_id": {
							Type: "objectId",
						},
						"orderId": {
							Type:   "string",
							Format: "uuid",
						},
						"customerId": {
							Type:  "string",
							Faker: "objectId",
						},
						"status": {
							Type: "string",
							Enum: []any{"pending", "processing", "shipped", "delivered", "cancelled"},
						},
						"items": {
							Type:     "array",
							MinItems: new(1),
							MaxItems: new(5),
							Items: &JSONSchema{
								Type: "object",
								Properties: map[string]*JSONSchema{
									"productId": {Type: "string", Format: "uuid"},
									"name":      {Type: "string", Faker: "product"},
									"quantity":  {Type: "integer", Minimum: new(float64(1)), Maximum: new(float64(10))},
									"price":     {Type: "number", Minimum: new(4.99), Maximum: new(499.99)},
								},
								Required: []string{"productId", "name", "quantity", "price"},
							},
						},
						"totalAmount": {
							Type:    "number",
							Minimum: new(10.0),
							Maximum: new(2500.0),
						},
						"currency": {
							Type: "string",
							Enum: []any{"USD", "EUR", "GBP", "CAD", "JPY"},
						},
						"paymentMethod": {
							Type: "string",
							Enum: []any{"credit_card", "debit_card", "paypal", "apple_pay", "bank_transfer"},
						},
						"shippingAddress": {
							Type: "object",
							Properties: map[string]*JSONSchema{
								"street":  {Type: "string", Faker: "street"},
								"city":    {Type: "string", Faker: "city"},
								"country": {Type: "string", Faker: "country"},
								"zipCode": {Type: "string", Faker: "zipCode"},
							},
						},
						"createdAt": {
							Type:   "string",
							Format: "date-time",
						},
					},
					Required: []string{"orderId", "status", "items", "totalAmount", "createdAt"},
				},
			},
		},
	}
}

// LoadSchemaFromFile reads and parses a JSON schema from a given file path.
func LoadSchemaFromFile(filePath string, defaultDB string) (*SchemaConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema file %s: %w", filePath, err)
	}
	return ParseSchema(data, defaultDB)
}

// ParseSchema parses a JSON schema from raw bytes into a SchemaConfig.
func ParseSchema(data []byte, defaultDB string) (*SchemaConfig, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("schema data is empty")
	}

	var raw rawSchemaConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		// Attempt parsing as a plain array of collection configs
		var arr []rawCollectionConfig
		if errArr := json.Unmarshal(data, &arr); errArr == nil && len(arr) > 0 {
			cfg := &SchemaConfig{
				Database: defaultDB,
			}
			for _, item := range arr {
				coll, err := parseRawCollection(item)
				if err != nil {
					return nil, err
				}
				cfg.Collections = append(cfg.Collections, coll)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("invalid json in schema file: %w", err)
	}

	cfg := &SchemaConfig{
		Database: raw.Database,
	}
	if cfg.Database == "" {
		cfg.Database = defaultDB
	}

	// 1. Check if `collections` field is present
	if len(raw.Collections) > 0 {
		// Case A: collections is an array of objects
		var collList []rawCollectionConfig
		if err := json.Unmarshal(raw.Collections, &collList); err == nil {
			for _, item := range collList {
				coll, err := parseRawCollection(item)
				if err != nil {
					return nil, err
				}
				cfg.Collections = append(cfg.Collections, coll)
			}
			if len(cfg.Collections) > 0 {
				return cfg, nil
			}
		}

		// Case B: collections is a map of string -> rawCollectionConfig or string -> JSONSchema
		var collMap map[string]json.RawMessage
		if err := json.Unmarshal(raw.Collections, &collMap); err == nil {
			for name, rawVal := range collMap {
				var item rawCollectionConfig
				if err := json.Unmarshal(rawVal, &item); err == nil && (item.Schema != nil || item.Properties != nil) {
					item.Name = name
					coll, err := parseRawCollection(item)
					if err != nil {
						return nil, err
					}
					cfg.Collections = append(cfg.Collections, coll)
				} else {
					var s JSONSchema
					if err := json.Unmarshal(rawVal, &s); err == nil {
						cfg.Collections = append(cfg.Collections, CollectionConfig{
							Name:   name,
							Weight: 1.0,
							CRUD:   DefaultCRUDWeights(),
							Schema: &s,
						})
					}
				}
			}
			if len(cfg.Collections) > 0 {
				return cfg, nil
			}
		}
	}

	// 2. Case C: Single collection schema at root
	if raw.Type != nil || raw.Properties != nil {
		collName := raw.Title
		if collName == "" {
			collName = "documents"
		}
		var s JSONSchema
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, fmt.Errorf("failed to parse root schema: %w", err)
		}
		weight := 1.0
		if raw.Weight != nil && *raw.Weight > 0 {
			weight = *raw.Weight
		}
		crud := DefaultCRUDWeights()
		if raw.CRUD != nil {
			crud = *raw.CRUD
		}
		cfg.Collections = append(cfg.Collections, CollectionConfig{
			Name:   collName,
			Weight: weight,
			CRUD:   crud,
			Schema: &s,
		})
		return cfg, nil
	}

	return nil, fmt.Errorf("no collections or valid schema definition found in schema file")
}

func parseRawCollection(item rawCollectionConfig) (CollectionConfig, error) {
	if item.Name == "" {
		return CollectionConfig{}, fmt.Errorf("collection name must not be empty")
	}

	weight := 1.0
	if item.Weight != nil && *item.Weight > 0 {
		weight = *item.Weight
	} else if item.Ratio != nil && *item.Ratio > 0 {
		weight = *item.Ratio
	} else if item.Percent != nil && *item.Percent > 0 {
		weight = *item.Percent
	}

	crud := DefaultCRUDWeights()
	if item.CRUD != nil {
		crud = *item.CRUD
	}

	schema := item.Schema
	if schema == nil {
		// If inline properties are defined
		schema = &JSONSchema{
			Type:       item.Type,
			Properties: item.Properties,
			Required:   item.Required,
		}
		if schema.Type == nil {
			schema.Type = "object"
		}
	}

	return CollectionConfig{
		Name:   item.Name,
		Weight: weight,
		CRUD:   crud,
		Schema: schema,
	}, nil
}
