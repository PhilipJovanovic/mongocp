package main

import (
	"net/http"
	"strings"
)

// handleOpenAPI serves the OpenAPI 3.1 spec that a Custom GPT imports as an
// Action. The server URL comes from PUBLIC_URL, falling back to the request
// host.
func (a *app) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	serverURL := a.publicURL
	if serverURL == "" {
		scheme := "https"
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			scheme = proto
		} else if r.TLS == nil {
			scheme = "http"
		}
		serverURL = scheme + "://" + r.Host
	}
	spec := strings.ReplaceAll(openAPISpec, "{{SERVER_URL}}", serverURL)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(spec))
}

const openAPISpec = `{
  "openapi": "3.1.0",
  "info": {
    "title": "MongoCP",
    "description": "A control plane for a MongoDB database. Lets you manage collections, read and write documents, and run aggregation pipelines for analytics. Document ids (_id) are returned as 24-character hex strings and can be passed back as plain strings in filters.",
    "version": "1.0.0"
  },
  "servers": [{"url": "{{SERVER_URL}}"}],
  "paths": {
    "/collections": {
      "get": {
        "operationId": "listCollections",
        "summary": "List all collections in the database",
        "responses": {"200": {"description": "Collection names", "content": {"application/json": {"schema": {"type": "object", "properties": {"collections": {"type": "array", "items": {"type": "string"}}}}}}}}
      },
      "post": {
        "operationId": "createCollection",
        "summary": "Create a new collection",
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {
            "type": "object",
            "required": ["name"],
            "properties": {
              "name": {"type": "string", "description": "Collection name (letters, digits, dots, dashes, underscores)"},
              "validator": {"type": "object", "description": "Optional MongoDB JSON schema validator, e.g. {\"$jsonSchema\": {...}}"}
            }
          }}}
        },
        "responses": {"201": {"description": "Created"}}
      }
    },
    "/collections/{name}": {
      "delete": {
        "operationId": "dropCollection",
        "summary": "Drop (permanently delete) a collection and all of its documents. Ask the user for confirmation before calling this.",
        "parameters": [{"name": "name", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "Dropped"}}
      }
    },
    "/collections/{name}/documents": {
      "post": {
        "operationId": "insertDocuments",
        "summary": "Insert one or more documents into a collection",
        "parameters": [{"name": "name", "in": "path", "required": true, "schema": {"type": "string"}}],
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {
            "type": "object",
            "required": ["documents"],
            "properties": {"documents": {"type": "array", "description": "Documents to insert", "items": {"type": "object"}}}
          }}}
        },
        "responses": {"201": {"description": "Inserted ids", "content": {"application/json": {"schema": {"type": "object", "properties": {"insertedCount": {"type": "integer"}, "insertedIds": {"type": "array", "items": {"type": "string"}}}}}}}}
      }
    },
    "/collections/{name}/query": {
      "post": {
        "operationId": "queryDocuments",
        "summary": "Find documents in a collection using a MongoDB filter",
        "parameters": [{"name": "name", "in": "path", "required": true, "schema": {"type": "string"}}],
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {
            "type": "object",
            "properties": {
              "filter": {"type": "object", "description": "MongoDB query filter, e.g. {\"status\": \"open\", \"age\": {\"$gt\": 30}}. Empty object matches everything."},
              "projection": {"type": "object", "description": "Fields to include/exclude, e.g. {\"name\": 1}"},
              "sort": {"type": "object", "description": "Sort spec, e.g. {\"createdAt\": -1}"},
              "limit": {"type": "integer", "description": "Max documents to return (default 50, max 1000)"},
              "skip": {"type": "integer", "description": "Documents to skip, for pagination"}
            }
          }}}
        },
        "responses": {"200": {"description": "Matching documents", "content": {"application/json": {"schema": {"type": "object", "properties": {"count": {"type": "integer"}, "documents": {"type": "array", "items": {"type": "object"}}}}}}}}
      }
    },
    "/collections/{name}/update": {
      "post": {
        "operationId": "updateDocuments",
        "summary": "Update documents matching a filter. Plain field maps are applied as $set; update operators like $inc are passed through.",
        "parameters": [{"name": "name", "in": "path", "required": true, "schema": {"type": "string"}}],
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {
            "type": "object",
            "required": ["filter", "update"],
            "properties": {
              "filter": {"type": "object", "description": "Which documents to update"},
              "update": {"type": "object", "description": "New field values, or an update document with operators like {\"$inc\": {\"count\": 1}}"},
              "many": {"type": "boolean", "description": "Update all matches instead of just the first (default false)"},
              "upsert": {"type": "boolean", "description": "Insert the document if nothing matches (default false)"}
            }
          }}}
        },
        "responses": {"200": {"description": "Update result", "content": {"application/json": {"schema": {"type": "object", "properties": {"matchedCount": {"type": "integer"}, "modifiedCount": {"type": "integer"}, "upsertedCount": {"type": "integer"}}}}}}}
      }
    },
    "/collections/{name}/delete": {
      "post": {
        "operationId": "deleteDocuments",
        "summary": "Delete documents matching a filter. Ask the user for confirmation before deleting many documents.",
        "parameters": [{"name": "name", "in": "path", "required": true, "schema": {"type": "string"}}],
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {
            "type": "object",
            "required": ["filter"],
            "properties": {
              "filter": {"type": "object", "description": "Which documents to delete"},
              "many": {"type": "boolean", "description": "Delete all matches instead of just the first (default false)"}
            }
          }}}
        },
        "responses": {"200": {"description": "Delete result", "content": {"application/json": {"schema": {"type": "object", "properties": {"deletedCount": {"type": "integer"}}}}}}}
      }
    },
    "/collections/{name}/aggregate": {
      "post": {
        "operationId": "aggregateDocuments",
        "summary": "Run a MongoDB aggregation pipeline on a collection for analytics ($match, $group, $sort, $lookup, ...). $out and $merge are not allowed.",
        "parameters": [{"name": "name", "in": "path", "required": true, "schema": {"type": "string"}}],
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {
            "type": "object",
            "required": ["pipeline"],
            "properties": {"pipeline": {"type": "array", "description": "Aggregation stages, e.g. [{\"$group\": {\"_id\": \"$status\", \"total\": {\"$sum\": 1}}}]", "items": {"type": "object"}}}
          }}}
        },
        "responses": {"200": {"description": "Aggregation results", "content": {"application/json": {"schema": {"type": "object", "properties": {"count": {"type": "integer"}, "results": {"type": "array", "items": {"type": "object"}}}}}}}}
      }
    }
  },
  "components": {
    "schemas": {},
    "securitySchemes": {
      "bearerAuth": {"type": "http", "scheme": "bearer"}
    }
  },
  "security": [{"bearerAuth": []}]
}`
