package main

import (
	"net/http"
	"strings"
)

// handleOpenAPI serves the OpenAPI 3.1 spec that a Custom GPT imports as an
// Action. The server URL comes from PUBLIC_URL, falling back to the request
// host.
//
// Every operation is a flat POST with the collection inside the JSON body:
// the GPT Actions importer reliably picks up body-only schemas but tends to
// drop request bodies on operations that mix path parameters with a body.
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
    "description": "REST API for a MongoDB database: manage collections, read and write documents, and run aggregation pipelines for analytics. Every operation takes a single JSON request body; the target collection is always the \"collection\" field in that body. Documents are plain JSON objects with arbitrary fields - no BSON or extended JSON syntax needed. Write dates as ISO 8601 strings like \"2026-07-14T10:00:00Z\". Document ids (_id) are returned as 24-character hex strings and can be passed back as plain strings in filters, e.g. {\"_id\": \"665f1c...\"}.",
    "version": "1.2.0"
  },
  "servers": [{"url": "{{SERVER_URL}}"}],
  "paths": {
    "/collections": {
      "get": {
        "operationId": "listCollections",
        "summary": "List all collections in the database",
        "responses": {
          "200": {
            "description": "Collection names",
            "content": {"application/json": {"schema": {
              "type": "object",
              "properties": {"collections": {"type": "array", "items": {"type": "string"}}}
            }}}
          }
        }
      }
    },
    "/collections/create": {
      "post": {
        "operationId": "createCollection",
        "summary": "Create a new, empty collection",
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {
              "type": "object",
              "required": ["collection"],
              "properties": {
                "collection": {"type": "string", "description": "Name of the collection to create (letters, digits, dots, dashes, underscores)"}
              }
            },
            "example": {"collection": "todos"}
          }}
        },
        "responses": {"201": {"description": "Created"}}
      }
    },
    "/collections/drop": {
      "post": {
        "operationId": "dropCollection",
        "summary": "Drop (permanently delete) a collection and all of its documents. Ask the user for confirmation before calling this.",
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {
              "type": "object",
              "required": ["collection"],
              "properties": {
                "collection": {"type": "string", "description": "Name of the collection to drop"}
              }
            },
            "example": {"collection": "todos"}
          }}
        },
        "responses": {"200": {"description": "Dropped"}}
      }
    },
    "/documents/insert": {
      "post": {
        "operationId": "insertDocuments",
        "summary": "Insert one or more documents into a collection. Creates the collection automatically if it does not exist yet.",
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {
              "type": "object",
              "required": ["collection", "documents"],
              "properties": {
                "collection": {"type": "string", "description": "Target collection, e.g. todos"},
                "documents": {
                  "type": "array",
                  "description": "The documents to insert. Each document is a plain JSON object with any fields you like; nested objects and arrays are allowed. Do not include an _id field, it is generated automatically.",
                  "items": {"type": "object", "additionalProperties": true}
                }
              }
            },
            "example": {
              "collection": "todos",
              "documents": [
                {"title": "Milch kaufen", "done": false, "list": "Privat", "createdAt": "2026-07-14T10:00:00Z"},
                {"title": "Steuererklaerung machen", "done": false, "list": "Privat", "createdAt": "2026-07-14T10:00:00Z"}
              ]
            }
          }}
        },
        "responses": {
          "201": {
            "description": "Ids of the inserted documents",
            "content": {"application/json": {"schema": {
              "type": "object",
              "properties": {
                "insertedCount": {"type": "integer"},
                "insertedIds": {"type": "array", "items": {"type": "string"}}
              }
            }}}
          }
        }
      }
    },
    "/documents/query": {
      "post": {
        "operationId": "queryDocuments",
        "summary": "Find documents in a collection using a MongoDB filter. Send an empty filter {} to list everything.",
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {
              "type": "object",
              "required": ["collection"],
              "properties": {
                "collection": {"type": "string", "description": "Collection to search"},
                "filter": {"type": "object", "additionalProperties": true, "description": "MongoDB query filter, e.g. {\"done\": false} or {\"amount\": {\"$gt\": 100}}. Empty object {} matches all documents."},
                "projection": {"type": "object", "additionalProperties": true, "description": "Optional. Fields to include (1) or exclude (0), e.g. {\"title\": 1}"},
                "sort": {"type": "object", "additionalProperties": true, "description": "Optional. Sort spec: 1 ascending, -1 descending, e.g. {\"createdAt\": -1}"},
                "limit": {"type": "integer", "description": "Optional. Max documents to return (default 50, max 1000)"},
                "skip": {"type": "integer", "description": "Optional. Documents to skip, for pagination"}
              }
            },
            "example": {"collection": "todos", "filter": {"done": false}, "sort": {"createdAt": -1}, "limit": 20}
          }}
        },
        "responses": {
          "200": {
            "description": "Matching documents",
            "content": {"application/json": {"schema": {
              "type": "object",
              "properties": {
                "count": {"type": "integer"},
                "documents": {"type": "array", "items": {"type": "object", "additionalProperties": true}}
              }
            }}}
          }
        }
      }
    },
    "/documents/update": {
      "post": {
        "operationId": "updateDocuments",
        "summary": "Update documents matching a filter. A plain object of field values is applied as $set; MongoDB update operators like $inc or $push are passed through.",
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {
              "type": "object",
              "required": ["collection", "filter", "update"],
              "properties": {
                "collection": {"type": "string", "description": "Collection to update"},
                "filter": {"type": "object", "additionalProperties": true, "description": "Which documents to update, e.g. {\"_id\": \"665f1c...\"} or {\"done\": false}"},
                "update": {"type": "object", "additionalProperties": true, "description": "New field values as a plain object, e.g. {\"done\": true}, or an update document with operators, e.g. {\"$push\": {\"tags\": \"neu\"}}"},
                "many": {"type": "boolean", "description": "Update all matches instead of just the first (default false)"},
                "upsert": {"type": "boolean", "description": "Insert the document if nothing matches (default false)"}
              }
            },
            "example": {"collection": "todos", "filter": {"title": "Milch kaufen"}, "update": {"done": true}}
          }}
        },
        "responses": {
          "200": {
            "description": "Update result",
            "content": {"application/json": {"schema": {
              "type": "object",
              "properties": {
                "matchedCount": {"type": "integer"},
                "modifiedCount": {"type": "integer"},
                "upsertedCount": {"type": "integer"}
              }
            }}}
          }
        }
      }
    },
    "/documents/delete": {
      "post": {
        "operationId": "deleteDocuments",
        "summary": "Delete documents matching a filter. Ask the user for confirmation before deleting many documents.",
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {
              "type": "object",
              "required": ["collection", "filter"],
              "properties": {
                "collection": {"type": "string", "description": "Collection to delete from"},
                "filter": {"type": "object", "additionalProperties": true, "description": "Which documents to delete, e.g. {\"_id\": \"665f1c...\"}"},
                "many": {"type": "boolean", "description": "Delete all matches instead of just the first (default false)"}
              }
            },
            "example": {"collection": "todos", "filter": {"done": true}, "many": true}
          }}
        },
        "responses": {
          "200": {
            "description": "Delete result",
            "content": {"application/json": {"schema": {
              "type": "object",
              "properties": {"deletedCount": {"type": "integer"}}
            }}}
          }
        }
      }
    },
    "/documents/aggregate": {
      "post": {
        "operationId": "aggregateDocuments",
        "summary": "Run a MongoDB aggregation pipeline on a collection for analytics and reports ($match, $group, $sort, $lookup, ...). $out and $merge are not allowed.",
        "requestBody": {
          "required": true,
          "content": {"application/json": {
            "schema": {
              "type": "object",
              "required": ["collection", "pipeline"],
              "properties": {
                "collection": {"type": "string", "description": "Collection to aggregate over"},
                "pipeline": {
                  "type": "array",
                  "description": "Aggregation stages in order, each a plain JSON object",
                  "items": {"type": "object", "additionalProperties": true}
                }
              }
            },
            "example": {
              "collection": "todos",
              "pipeline": [
                {"$match": {"done": false}},
                {"$group": {"_id": "$list", "count": {"$sum": 1}}},
                {"$sort": {"count": -1}}
              ]
            }
          }}
        },
        "responses": {
          "200": {
            "description": "Aggregation results",
            "content": {"application/json": {"schema": {
              "type": "object",
              "properties": {
                "count": {"type": "integer"},
                "results": {"type": "array", "items": {"type": "object", "additionalProperties": true}}
              }
            }}}
          }
        }
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
