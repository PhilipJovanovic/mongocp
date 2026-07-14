package main

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// sanitize converts BSON-specific types into plain JSON-friendly values so
// responses are easy for ChatGPT to read: ObjectIDs become hex strings and
// datetimes become RFC 3339 strings.
func sanitize(v any) any {
	switch t := v.(type) {
	case bson.M:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = sanitize(val)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = sanitize(val)
		}
		return out
	case bson.D:
		out := make(map[string]any, len(t))
		for _, e := range t {
			out[e.Key] = sanitize(e.Value)
		}
		return out
	case bson.A:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = sanitize(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = sanitize(val)
		}
		return out
	case []bson.M:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = sanitize(val)
		}
		return out
	case bson.ObjectID:
		return t.Hex()
	case bson.DateTime:
		return t.Time().UTC().Format(time.RFC3339)
	case bson.Decimal128:
		return t.String()
	case bson.Binary:
		return map[string]any{"$binary": true, "length": len(t.Data)}
	default:
		return v
	}
}

// normalizeValues walks a JSON value from a request and converts it into what
// the database expects. LLM clients express ids and dates in many shapes, so
// all of these are accepted:
//   - 24-char hex strings under an "_id" key (also inside operators like $in)
//     become ObjectIDs
//   - extended-JSON {"$oid": "..."} becomes an ObjectID
//   - extended-JSON {"$date": "RFC 3339 string" | epoch-millis} becomes a
//     time.Time
func normalizeValues(v any, underID bool) any {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 1 {
			if raw, ok := t["$oid"]; ok {
				if s, ok := raw.(string); ok {
					if oid, err := bson.ObjectIDFromHex(s); err == nil {
						return oid
					}
				}
			}
			if raw, ok := t["$date"]; ok {
				switch d := raw.(type) {
				case string:
					if ts, err := time.Parse(time.RFC3339Nano, d); err == nil {
						return ts.UTC()
					}
				case float64:
					return time.UnixMilli(int64(d)).UTC()
				}
			}
		}
		out := make(map[string]any, len(t))
		for k, val := range t {
			childUnderID := k == "_id" || (underID && len(k) > 0 && k[0] == '$')
			out[k] = normalizeValues(val, childUnderID)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalizeValues(val, underID)
		}
		return out
	case string:
		if underID {
			if oid, err := bson.ObjectIDFromHex(t); err == nil {
				return oid
			}
		}
		return t
	default:
		return v
	}
}
