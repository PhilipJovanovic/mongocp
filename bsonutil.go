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

// normalizeIDs walks a JSON value and converts 24-char hex strings under an
// "_id" key (including inside operators like {"_id": {"$in": [...]}}) into
// real ObjectIDs, so ChatGPT can reference documents by their hex id string.
func normalizeIDs(v any, underID bool) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			childUnderID := k == "_id" || (underID && len(k) > 0 && k[0] == '$')
			out[k] = normalizeIDs(val, childUnderID)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normalizeIDs(val, underID)
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
