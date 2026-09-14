package load

import (
	"crypto/rand"
	"fmt"
	"math"
	mathrand "math/rand"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Generator produces random BSON data, update patches, and query filters based on JSONSchema.
type Generator struct {
	mu  sync.Mutex
	rng *mathrand.Rand
}

// NewGenerator creates a new schema data generator.
func NewGenerator() *Generator {
	return &Generator{
		rng: mathrand.New(mathrand.NewSource(time.Now().UnixNano())),
	}
}

// GenerateDocument creates a full BSON document for a collection schema (Create operation / Initial Load).
func (g *Generator) GenerateDocument(schema *JSONSchema) bson.M {
	if schema == nil {
		return bson.M{"_id": bson.NewObjectID()}
	}

	val := g.GenerateValue(schema)
	if doc, ok := val.(bson.M); ok {
		// Ensure _id exists if not generated
		if _, hasID := doc["_id"]; !hasID {
			doc["_id"] = bson.NewObjectID()
		}
		return doc
	}

	return bson.M{"_id": bson.NewObjectID(), "data": val}
}

// GenerateUpdatePatch creates a $set update payload targeting mutable fields.
func (g *Generator) GenerateUpdatePatch(schema *JSONSchema) bson.M {
	setFields := bson.M{}

	if schema != nil && len(schema.Properties) > 0 {
		var mutableFields []string
		for k := range schema.Properties {
			// Skip immutable fields
			if k == "_id" || k == "id" || k == "orderId" || k == "userId" || k == "customerId" || k == "createdAt" {
				continue
			}
			mutableFields = append(mutableFields, k)
		}

		if len(mutableFields) > 0 {
			g.mu.Lock()
			// Pick 1 to 3 mutable fields
			numFields := 1 + g.rng.Intn(int(math.Min(float64(len(mutableFields)), 3)))
			g.rng.Shuffle(len(mutableFields), func(i, j int) {
				mutableFields[i], mutableFields[j] = mutableFields[j], mutableFields[i]
			})
			g.mu.Unlock()

			for i := range numFields {
				field := mutableFields[i]
				setFields[field] = g.GenerateValue(schema.Properties[field])
			}
		}
	}

	// Always update updatedAt timestamp if present or as a standard timestamp
	if schema != nil && schema.Properties != nil && schema.Properties["updatedAt"] != nil {
		setFields["updatedAt"] = time.Now().UTC().Format(time.RFC3339)
	} else if len(setFields) == 0 {
		setFields["updatedAt"] = time.Now().UTC().Format(time.RFC3339)
		setFields["status"] = "updated"
	}

	return bson.M{"$set": setFields}
}

// GenerateMissQuery produces a query filter for a non-existent document.
func (g *Generator) GenerateMissQuery() bson.M {
	return bson.M{"_id": bson.NewObjectID()}
}

// GenerateValue recursively generates a value matching a JSONSchema.
func (g *Generator) GenerateValue(schema *JSONSchema) any {
	if schema == nil {
		return nil
	}

	// 1. If Default is provided
	if schema.Default != nil {
		return schema.Default
	}

	// 2. If Enum is provided
	if len(schema.Enum) > 0 {
		g.mu.Lock()
		idx := g.rng.Intn(len(schema.Enum))
		g.mu.Unlock()
		return schema.Enum[idx]
	}

	// 3. If Faker is specified
	if schema.Faker != "" {
		return g.generateFaker(schema.Faker)
	}

	// 4. If Format is specified
	if schema.Format != "" {
		return g.generateFormat(schema.Format)
	}

	// 5. Match by Type
	typeName := "string"
	switch t := schema.Type.(type) {
	case string:
		typeName = t
	case []any:
		if len(t) > 0 {
			if s, ok := t[0].(string); ok {
				typeName = s
			}
		}
	}

	switch strings.ToLower(typeName) {
	case "objectid", "object_id", "oid":
		return bson.NewObjectID()
	case "object":
		return g.generateObject(schema)
	case "array":
		return g.generateArray(schema)
	case "integer", "int", "long":
		return g.generateInteger(schema)
	case "number", "float", "double":
		return g.generateNumber(schema)
	case "boolean", "bool":
		return g.generateBoolean()
	case "null":
		return nil
	case "string":
		return g.generateString(schema)
	default:
		if len(schema.Properties) > 0 {
			return g.generateObject(schema)
		}
		if schema.Items != nil {
			return g.generateArray(schema)
		}
		return g.generateString(schema)
	}
}

func (g *Generator) generateObject(schema *JSONSchema) bson.M {
	doc := bson.M{}
	if schema.Properties == nil {
		return doc
	}

	requiredSet := make(map[string]bool)
	for _, r := range schema.Required {
		requiredSet[r] = true
	}

	for k, propSchema := range schema.Properties {
		if k == "_id" && (propSchema.Type == "objectId" || propSchema.Type == nil) {
			doc[k] = bson.NewObjectID()
			continue
		}

		// Include if required, or ~85% chance for optional fields
		include := requiredSet[k]
		if !include {
			g.mu.Lock()
			include = g.rng.Float64() < 0.85
			g.mu.Unlock()
		}

		if include {
			doc[k] = g.GenerateValue(propSchema)
		}
	}

	return doc
}

func (g *Generator) generateArray(schema *JSONSchema) bson.A {
	minItems := 1
	maxItems := 3

	if schema.MinItems != nil && *schema.MinItems >= 0 {
		minItems = *schema.MinItems
	}
	if schema.MaxItems != nil && *schema.MaxItems >= minItems {
		maxItems = *schema.MaxItems
	}

	g.mu.Lock()
	count := minItems
	if maxItems > minItems {
		count = minItems + g.rng.Intn(maxItems-minItems+1)
	}
	g.mu.Unlock()

	arr := make(bson.A, 0, count)
	for i := 0; i < count; i++ {
		if schema.Items != nil {
			arr = append(arr, g.GenerateValue(schema.Items))
		} else {
			arr = append(arr, g.generateWord())
		}
	}

	return arr
}

func (g *Generator) generateInteger(schema *JSONSchema) int64 {
	minVal := int64(0)
	maxVal := int64(1000)

	if schema.Minimum != nil {
		minVal = int64(*schema.Minimum)
	}
	if schema.Maximum != nil {
		maxVal = int64(*schema.Maximum)
	}
	if maxVal < minVal {
		maxVal = minVal + 100
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if maxVal == minVal {
		return minVal
	}
	return minVal + g.rng.Int63n(maxVal-minVal+1)
}

func (g *Generator) generateNumber(schema *JSONSchema) float64 {
	minVal := 0.0
	maxVal := 1000.0

	if schema.Minimum != nil {
		minVal = *schema.Minimum
	}
	if schema.Maximum != nil {
		maxVal = *schema.Maximum
	}
	if maxVal < minVal {
		maxVal = minVal + 100.0
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	raw := minVal + g.rng.Float64()*(maxVal-minVal)
	return math.Round(raw*100) / 100 // Round to 2 decimal places
}

func (g *Generator) generateBoolean() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.rng.Intn(2) == 1
}

func (g *Generator) generateString(schema *JSONSchema) string {
	minLen := 5
	maxLen := 12

	if schema.MinLength != nil && *schema.MinLength > 0 {
		minLen = *schema.MinLength
	}
	if schema.MaxLength != nil && *schema.MaxLength >= minLen {
		maxLen = *schema.MaxLength
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	length := minLen
	if maxLen > minLen {
		length = minLen + g.rng.Intn(maxLen-minLen+1)
	}

	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	var sb strings.Builder
	for i := 0; i < length; i++ {
		sb.WriteByte(chars[g.rng.Intn(len(chars))])
	}
	return sb.String()
}

func (g *Generator) generateFormat(format string) any {
	switch strings.ToLower(format) {
	case "date-time", "datetime":
		g.mu.Lock()
		offsetSec := g.rng.Int63n(365 * 24 * 3600)
		g.mu.Unlock()
		return time.Now().Add(-time.Duration(offsetSec) * time.Second).UTC().Format(time.RFC3339)
	case "date":
		g.mu.Lock()
		offsetSec := g.rng.Int63n(365 * 24 * 3600)
		g.mu.Unlock()
		return time.Now().Add(-time.Duration(offsetSec) * time.Second).UTC().Format("2006-01-02")
	case "email":
		return g.generateEmail()
	case "uuid":
		return g.generateUUID()
	case "ipv4":
		return g.generateIPv4()
	case "uri", "url":
		return "https://example.com/item/" + g.generateUUID()
	case "objectid":
		return bson.NewObjectID().Hex()
	default:
		return g.generateWord()
	}
}

func (g *Generator) generateFaker(faker string) any {
	switch strings.ToLower(faker) {
	case "firstname", "first_name":
		return g.pickRandom(firstNames)
	case "lastname", "last_name":
		return g.pickRandom(lastNames)
	case "name", "fullname", "full_name":
		return fmt.Sprintf("%s %s", g.pickRandom(firstNames), g.pickRandom(lastNames))
	case "username":
		return fmt.Sprintf("%s_%s%d", strings.ToLower(g.pickRandom(firstNames)), strings.ToLower(g.pickRandom(lastNames)), g.randInt(10, 999))
	case "email":
		return g.generateEmail()
	case "phone":
		return fmt.Sprintf("+1-555-%04d", g.randInt(1000, 9999))
	case "company":
		return g.pickRandom(companies)
	case "city":
		return g.pickRandom(cities)
	case "country":
		return g.pickRandom(countries)
	case "street":
		return fmt.Sprintf("%d %s St", g.randInt(100, 9999), g.pickRandom(streets))
	case "zipcode", "zip_code":
		return fmt.Sprintf("%05d", g.randInt(10000, 99999))
	case "product":
		return g.pickRandom(products)
	case "currency":
		return g.pickRandom(currencies)
	case "status":
		return g.pickRandom(statuses)
	case "word":
		return g.generateWord()
	case "sentence":
		return fmt.Sprintf("%s %s %s.", g.generateWord(), g.generateWord(), g.generateWord())
	case "uuid":
		return g.generateUUID()
	case "objectid":
		return bson.NewObjectID().Hex()
	default:
		return g.pickRandom(words)
	}
}

func (g *Generator) pickRandom(list []string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return list[g.rng.Intn(len(list))]
}

func (g *Generator) randInt(min, max int) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if max <= min {
		return min
	}
	return min + g.rng.Intn(max-min+1)
}

func (g *Generator) generateEmail() string {
	fn := strings.ToLower(g.pickRandom(firstNames))
	ln := strings.ToLower(g.pickRandom(lastNames))
	domain := g.pickRandom(domains)
	return fmt.Sprintf("%s.%s%d@%s", fn, ln, g.randInt(1, 999), domain)
}

func (g *Generator) generateWord() string {
	return g.pickRandom(words)
}

func (g *Generator) generateIPv4() string {
	return fmt.Sprintf("%d.%d.%d.%d", g.randInt(10, 192), g.randInt(0, 255), g.randInt(0, 255), g.randInt(1, 254))
}

func (g *Generator) generateUUID() string {
	var uuid [16]byte
	_, _ = rand.Read(uuid[:])
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16])
}

// Data pools for realistic test generation
var (
	firstNames = []string{"Alex", "Jordan", "Taylor", "Morgan", "Sam", "Chris", "Pat", "Robin", "Casey", "Avery", "Jamie", "Riley", "Jesse", "Dakota", "Reese"}
	lastNames  = []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Miller", "Davis", "Garcia", "Rodriguez", "Wilson", "Martinez", "Anderson", "Taylor", "Thomas", "Moore"}
	domains    = []string{"example.com", "mail.test", "corp.local", "inbox.net", "cloud.dev", "domain.org"}
	cities     = []string{"New York", "San Francisco", "Austin", "Seattle", "Chicago", "London", "Tokyo", "Berlin", "Toronto", "Sydney"}
	countries  = []string{"United States", "Canada", "United Kingdom", "Germany", "Japan", "Australia", "France", "Netherlands"}
	streets    = []string{"Market", "Main", "Maple", "Pine", "Cedar", "Broadway", "Oak", "Elm", "Washington", "Park"}
	companies  = []string{"Acme Corp", "Globex", "Initech", "Umbrella", "Cyberdyne", "Soylent", "Stark Industries", "Wayne Enterprises"}
	products   = []string{"Wireless Headphones", "Mechanical Keyboard", "USB-C Monitor", "Smart Watch", "Laptop Stand", "Desk Mat", "Ergonomic Mouse", "Webcam 4K", "Noise-Cancelling Earbuds"}
	currencies = []string{"USD", "EUR", "GBP", "CAD", "JPY"}
	statuses   = []string{"active", "pending", "completed", "archived", "suspended"}
	words      = []string{"database", "cluster", "sharding", "replica", "throughput", "latency", "pipeline", "aggregation", "index", "document", "collection", "session", "transaction"}
)
