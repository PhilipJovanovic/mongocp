package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	requestTimeout = 30 * time.Second
	defaultLimit   = 50
	maxLimit       = 1000
)

var collectionNameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,120}$`)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func validCollection(w http.ResponseWriter, name string) bool {
	if !collectionNameRe.MatchString(name) || strings.HasPrefix(name, "system.") {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid collection name %q", name))
		return false
	}
	return true
}

func reqContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), requestTimeout)
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /collections
func (a *app) handleListCollections(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := reqContext(r)
	defer cancel()

	names, err := a.db.ListCollectionNames(ctx, bson.M{})
	if err != nil {
		writeError(w, http.StatusBadGateway, "list collections: "+err.Error())
		return
	}
	if names == nil {
		names = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"collections": names})
}

// POST /collections  {"name": "...", "validator": {...}}
func (a *app) handleCreateCollection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string     `json:"name"`
		Validator flexObject `json:"validator"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !validCollection(w, req.Name) {
		return
	}

	ctx, cancel := reqContext(r)
	defer cancel()

	opts := options.CreateCollection()
	if req.Validator != nil {
		opts.SetValidator(map[string]any(req.Validator))
	}
	if err := a.db.CreateCollection(ctx, req.Name, opts); err != nil {
		writeError(w, http.StatusBadGateway, "create collection: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"created": req.Name})
}

// DELETE /collections/{name}
func (a *app) handleDropCollection(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validCollection(w, name) {
		return
	}

	ctx, cancel := reqContext(r)
	defer cancel()

	if err := a.db.Collection(name).Drop(ctx); err != nil {
		writeError(w, http.StatusBadGateway, "drop collection: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dropped": name})
}

// POST /collections/{name}/documents  {"documents": [{...}, ...]}
func (a *app) handleInsert(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validCollection(w, name) {
		return
	}
	var req struct {
		Documents flexObjectArray `json:"documents"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Documents) == 0 {
		writeError(w, http.StatusBadRequest, "documents must be a non-empty array")
		return
	}

	docs := make([]any, len(req.Documents))
	for i, d := range req.Documents {
		docs[i] = normalizeValues(d, false)
	}

	ctx, cancel := reqContext(r)
	defer cancel()

	res, err := a.db.Collection(name).InsertMany(ctx, docs)
	if err != nil {
		writeError(w, http.StatusBadGateway, "insert: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"insertedCount": len(res.InsertedIDs),
		"insertedIds":   sanitize(res.InsertedIDs),
	})
}

// POST /collections/{name}/query  {"filter": {...}, "projection": {...}, "sort": {...}, "limit": n, "skip": n}
func (a *app) handleQuery(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validCollection(w, name) {
		return
	}
	var req struct {
		Filter     flexObject `json:"filter"`
		Projection flexObject `json:"projection"`
		Sort       flexObject `json:"sort"`
		Limit      int64      `json:"limit"`
		Skip       int64      `json:"skip"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Filter == nil {
		req.Filter = flexObject{}
	}
	if req.Limit <= 0 {
		req.Limit = defaultLimit
	}
	if req.Limit > maxLimit {
		req.Limit = maxLimit
	}

	opts := options.Find().SetLimit(req.Limit).SetSkip(req.Skip)
	if req.Projection != nil {
		opts.SetProjection(req.Projection)
	}
	if req.Sort != nil {
		opts.SetSort(req.Sort)
	}

	ctx, cancel := reqContext(r)
	defer cancel()

	cur, err := a.db.Collection(name).Find(ctx, normalizeValues(map[string]any(req.Filter), false), opts)
	if err != nil {
		writeError(w, http.StatusBadGateway, "query: "+err.Error())
		return
	}
	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		writeError(w, http.StatusBadGateway, "query: "+err.Error())
		return
	}
	if docs == nil {
		docs = []bson.M{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":     len(docs),
		"documents": sanitize(docs),
	})
}

// POST /collections/{name}/update  {"filter": {...}, "update": {...}, "many": bool, "upsert": bool}
func (a *app) handleUpdate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validCollection(w, name) {
		return
	}
	var req struct {
		Filter flexObject `json:"filter"`
		Update flexObject `json:"update"`
		Many   bool       `json:"many"`
		Upsert bool       `json:"upsert"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Filter) == 0 && !req.Many {
		writeError(w, http.StatusBadRequest, "filter is required (set many=true to intentionally update all documents)")
		return
	}
	if len(req.Update) == 0 {
		writeError(w, http.StatusBadRequest, "update is required")
		return
	}

	// Wrap plain documents in $set so ChatGPT can send simple field updates.
	update := normalizeValues(map[string]any(req.Update), false)
	if !hasOperatorKeys(req.Update) {
		update = bson.M{"$set": update}
	}
	var filter any = map[string]any{}
	if req.Filter != nil {
		filter = normalizeValues(map[string]any(req.Filter), false)
	}

	ctx, cancel := reqContext(r)
	defer cancel()

	coll := a.db.Collection(name)
	opts := options.UpdateOne().SetUpsert(req.Upsert)
	manyOpts := options.UpdateMany().SetUpsert(req.Upsert)

	var matched, modified, upserted int64
	var upsertedID any
	if req.Many {
		res, err := coll.UpdateMany(ctx, filter, update, manyOpts)
		if err != nil {
			writeError(w, http.StatusBadGateway, "update: "+err.Error())
			return
		}
		matched, modified, upserted, upsertedID = res.MatchedCount, res.ModifiedCount, res.UpsertedCount, res.UpsertedID
	} else {
		res, err := coll.UpdateOne(ctx, filter, update, opts)
		if err != nil {
			writeError(w, http.StatusBadGateway, "update: "+err.Error())
			return
		}
		matched, modified, upserted, upsertedID = res.MatchedCount, res.ModifiedCount, res.UpsertedCount, res.UpsertedID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"matchedCount":  matched,
		"modifiedCount": modified,
		"upsertedCount": upserted,
		"upsertedId":    sanitize(upsertedID),
	})
}

// POST /collections/{name}/delete  {"filter": {...}, "many": bool}
func (a *app) handleDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validCollection(w, name) {
		return
	}
	var req struct {
		Filter flexObject `json:"filter"`
		Many   bool       `json:"many"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Filter) == 0 && !req.Many {
		writeError(w, http.StatusBadRequest, "filter is required (set many=true to intentionally delete all documents)")
		return
	}
	var filter any = map[string]any{}
	if req.Filter != nil {
		filter = normalizeValues(map[string]any(req.Filter), false)
	}

	ctx, cancel := reqContext(r)
	defer cancel()

	coll := a.db.Collection(name)
	var deleted int64
	if req.Many {
		res, err := coll.DeleteMany(ctx, filter)
		if err != nil {
			writeError(w, http.StatusBadGateway, "delete: "+err.Error())
			return
		}
		deleted = res.DeletedCount
	} else {
		res, err := coll.DeleteOne(ctx, filter)
		if err != nil {
			writeError(w, http.StatusBadGateway, "delete: "+err.Error())
			return
		}
		deleted = res.DeletedCount
	}
	writeJSON(w, http.StatusOK, map[string]any{"deletedCount": deleted})
}

// POST /collections/{name}/aggregate  {"pipeline": [{...}, ...]}
func (a *app) handleAggregate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !validCollection(w, name) {
		return
	}
	var req struct {
		Pipeline flexObjectArray `json:"pipeline"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Pipeline) == 0 {
		writeError(w, http.StatusBadRequest, "pipeline must be a non-empty array of stages")
		return
	}

	pipeline := make([]any, len(req.Pipeline))
	for i, stage := range req.Pipeline {
		for op := range stage {
			if op == "$out" || op == "$merge" {
				writeError(w, http.StatusBadRequest, op+" stages are not allowed")
				return
			}
		}
		pipeline[i] = normalizeValues(stage, false)
	}

	ctx, cancel := reqContext(r)
	defer cancel()

	cur, err := a.db.Collection(name).Aggregate(ctx, pipeline)
	if err != nil {
		writeError(w, http.StatusBadGateway, "aggregate: "+err.Error())
		return
	}
	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		writeError(w, http.StatusBadGateway, "aggregate: "+err.Error())
		return
	}
	if len(docs) > maxLimit {
		docs = docs[:maxLimit]
	}
	if docs == nil {
		docs = []bson.M{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":   len(docs),
		"results": sanitize(docs),
	})
}

func hasOperatorKeys(m flexObject) bool {
	for k := range m {
		if strings.HasPrefix(k, "$") {
			return true
		}
	}
	return false
}
